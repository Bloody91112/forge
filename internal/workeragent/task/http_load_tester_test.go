package task

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsValidHTTPURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "http", url: "http://example.com", want: true},
		{name: "https", url: "https://example.com/path", want: true},
		{name: "http with port", url: "http://localhost:8080", want: true},
		{name: "ftp", url: "ftp://example.com", want: false},
		{name: "missing scheme", url: "example.com", want: false},
		{name: "missing host", url: "http:///path", want: false},
		{name: "relative path", url: "/api/v1/test", want: false},
		{name: "empty", url: "", want: false},
		{name: "malformed URL", url: "http://%zz", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidHTTPURL(tt.url); got != tt.want {
				t.Errorf("isValidHTTPURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestHTTPLoadTester_HandleTask_InvalidPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "invalid JSON",
			payload: `{"url":`,
		},
		{
			name:    "invalid URL",
			payload: `{"url":"ftp://example.com","target_rps":1,"duration_seconds":1}`,
		},
		{
			name:    "zero target RPS",
			payload: `{"url":"http://example.com","target_rps":0,"duration_seconds":1}`,
		},
		{
			name:    "negative target RPS",
			payload: `{"url":"http://example.com","target_rps":-1,"duration_seconds":1}`,
		},
		{
			name:    "zero duration",
			payload: `{"url":"http://example.com","target_rps":1,"duration_seconds":0}`,
		},
		{
			name:    "negative duration",
			payload: `{"url":"http://example.com","target_rps":1,"duration_seconds":-1}`,
		},
	}

	tester := &HTTPLoadTester{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tester.HandleTask(context.Background(), Task{
				Payload: json.RawMessage(tt.payload),
			})

			if err != nil {
				t.Fatalf("HandleTask() error = %v, want nil", err)
			}

			if result.Error == nil {
				t.Fatal("expected validation error, got nil")
			}

			if result.Error.Code != ErrorCodeTaskInvalidPayload {
				t.Errorf(
					"error code = %q, want %q",
					result.Error.Code,
					ErrorCodeTaskInvalidPayload,
				)
			}

			if result.Error.Retryable {
				t.Error("validation error must not be retryable")
			}

			if result.Result != nil {
				t.Errorf("result = %v, want nil", result.Result)
			}
		})
	}
}

func TestHTTPLoadTester_MakeOneSecondLoad_HTTPResponses(t *testing.T) {
	var requests atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)

		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("service unavailable"))
	}))
	defer server.Close()

	tester := &HTTPLoadTester{
		client: server.Client(),
	}

	const targetRPS = 5

	resultsCh := make(chan SecondResult, 1)

	tester.makeOneSecondLoad(
		context.Background(),
		resultsCh,
		targetRPS,
		server.URL,
	)

	result := <-resultsCh

	if result.total != targetRPS {
		t.Errorf("total = %d, want %d", result.total, targetRPS)
	}

	if result.sent != targetRPS {
		t.Errorf("sent = %d, want %d", result.sent, targetRPS)
	}

	if result.completed != targetRPS {
		t.Errorf("completed = %d, want %d", result.completed, targetRPS)
	}

	if result.errors != 0 {
		t.Errorf("errors = %d, want 0", result.errors)
	}

	if result.dropped != 0 {
		t.Errorf("dropped = %d, want 0", result.dropped)
	}

	if got := result.statusCodes[http.StatusServiceUnavailable]; got != targetRPS {
		t.Errorf("503 responses = %d, want %d", got, targetRPS)
	}

	if got := requests.Load(); got != targetRPS {
		t.Errorf("server received %d requests, want %d", got, targetRPS)
	}
}

func TestHTTPLoadTester_MakeOneSecondLoad_TransportErrors(t *testing.T) {
	expectedErr := errors.New("connection refused")

	tester := &HTTPLoadTester{
		client: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				return nil, expectedErr
			}),
		},
	}

	const targetRPS = 3

	resultsCh := make(chan SecondResult, 1)

	tester.makeOneSecondLoad(
		context.Background(),
		resultsCh,
		targetRPS,
		"http://example.com",
	)

	result := <-resultsCh

	if result.total != targetRPS {
		t.Errorf("total = %d, want %d", result.total, targetRPS)
	}

	if result.sent != targetRPS {
		t.Errorf("sent = %d, want %d", result.sent, targetRPS)
	}

	if result.errors != targetRPS {
		t.Errorf("errors = %d, want %d", result.errors, targetRPS)
	}

	if result.completed != 0 {
		t.Errorf("completed = %d, want 0", result.completed)
	}

	if result.dropped != 0 {
		t.Errorf("dropped = %d, want 0", result.dropped)
	}
}

func TestHTTPLoadTester_MakeOneSecondLoad_CancelsRequestsAfterOneSecond(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	requestCanceled := make(chan struct{}, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}

		<-r.Context().Done()

		requestCanceled <- struct{}{}
	}))
	defer server.Close()

	tester := &HTTPLoadTester{
		client: server.Client(),
	}

	resultsCh := make(chan SecondResult, 1)

	go tester.makeOneSecondLoad(
		context.Background(),
		resultsCh,
		1,
		server.URL,
	)

	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP request was not started")
	}

	select {
	case <-requestCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP request was not canceled after the batch deadline")
	}

	var result SecondResult

	select {
	case result = <-resultsCh:
	case <-time.After(2 * time.Second):
		t.Fatal("batch did not return its result")
	}

	if result.total != 1 {
		t.Errorf("total = %d, want 1", result.total)
	}

	if result.sent != 1 {
		t.Errorf("sent = %d, want 1", result.sent)
	}

	if result.errors != 1 {
		t.Errorf("errors = %d, want 1", result.errors)
	}

	if result.completed != 0 {
		t.Errorf("completed = %d, want 0", result.completed)
	}
}

func TestHTTPLoadTester_MakeOneSecondLoad_DropsRequestsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tester := &HTTPLoadTester{
		client: &http.Client{},
	}

	resultsCh := make(chan SecondResult, 1)

	const targetRPS = 3

	tester.makeOneSecondLoad(ctx, resultsCh, targetRPS, "http://example.com")

	result := <-resultsCh

	if result.total != targetRPS {
		t.Errorf("total = %d, want %d", result.total, targetRPS)
	}

	if result.dropped != targetRPS {
		t.Errorf("dropped = %d, want %d", result.dropped, targetRPS)
	}

	if result.sent != 0 {
		t.Errorf("sent = %d, want 0", result.sent)
	}

	if result.errors != 0 {
		t.Errorf("errors = %d, want 0", result.errors)
	}
}

func TestHTTPLoadTester_CollectResults(t *testing.T) {
	resultsCh := make(chan SecondResult, 2)

	resultsCh <- SecondResult{
		total:        3,
		sent:         2,
		completed:    1,
		errors:       1,
		dropped:      1,
		latencyTotal: 25 * time.Millisecond,
	}

	resultsCh <- SecondResult{
		total:        3,
		sent:         3,
		completed:    2,
		errors:       1,
		latencyTotal: 75 * time.Millisecond,
	}

	close(resultsCh)

	tester := &HTTPLoadTester{}

	result := tester.collectResults(5, 2, resultsCh)

	if result.TargetRPS != 5 {
		t.Errorf("target RPS = %d, want 5", result.TargetRPS)
	}

	if result.DurationSeconds != 2 {
		t.Errorf("duration = %d, want 2", result.DurationSeconds)
	}

	if result.TotalSent != 5 {
		t.Errorf("total sent = %d, want 5", result.TotalSent)
	}

	if result.Completed != 3 {
		t.Errorf("completed = %d, want 3", result.Completed)
	}

	if result.TransportErrors != 2 {
		t.Errorf("transport errors = %d, want 2", result.TransportErrors)
	}

	if result.Dropped != 1 {
		t.Errorf("dropped = %d, want 1", result.Dropped)
	}

	if result.LatencyTotalMs != 100 {
		t.Errorf("total latency = %f, want 100", result.LatencyTotalMs)
	}

	if result.AvgLatencyMs != 100.0/3.0 {
		t.Errorf("average latency = %f, want %f", result.AvgLatencyMs, 100.0/3.0)
	}

	if result.ActualRPS != 2.5 {
		t.Errorf("actual RPS = %f, want 2.5", result.ActualRPS)
	}
}

func TestHTTPLoadTester_CollectResultsWithoutCompletedRequests(t *testing.T) {
	resultsCh := make(chan SecondResult, 1)

	resultsCh <- SecondResult{
		total:   3,
		sent:    2,
		errors:  1,
		dropped: 1,
	}

	close(resultsCh)

	tester := &HTTPLoadTester{}

	result := tester.collectResults(5, 2, resultsCh)

	if result.Completed != 0 {
		t.Errorf("completed = %d, want 0", result.Completed)
	}

	if result.AvgLatencyMs != 0 {
		t.Errorf("average latency = %f, want 0", result.AvgLatencyMs)
	}

	if result.TotalSent != 2 {
		t.Errorf("total sent = %d, want 2", result.TotalSent)
	}
}

func TestHTTPLoadTester_HandleTask_HTTPResponses(t *testing.T) {
	var requests atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	tester := &HTTPLoadTester{}

	task := newHTTPLoadTesterTask(t, server.URL, 4, 1)

	result, err := tester.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("HandleTask() error = %v, want nil", err)
	}

	if result.Error != nil {
		t.Fatalf("HandleTask() returned service error: %+v", result.Error)
	}

	loadResult, ok := result.Result.(HTTPLoadTesterResult)
	if !ok {
		t.Fatalf("result type = %T, want HTTPLoadTesterResult", result.Result)
	}

	if loadResult.TargetRPS != 4 {
		t.Errorf("target RPS = %d, want 4", loadResult.TargetRPS)
	}

	if loadResult.DurationSeconds != 1 {
		t.Errorf("duration = %d, want 1", loadResult.DurationSeconds)
	}

	if loadResult.TotalSent < 4 {
		t.Errorf("total sent = %d, want at least 4", loadResult.TotalSent)
	}

	if loadResult.Completed != loadResult.TotalSent {
		t.Errorf(
			"completed = %d, total sent = %d",
			loadResult.Completed,
			loadResult.TotalSent,
		)
	}

	if loadResult.TransportErrors != 0 {
		t.Errorf("transport errors = %d, want 0", loadResult.TransportErrors)
	}

	if loadResult.Dropped != 0 {
		t.Errorf("dropped = %d, want 0", loadResult.Dropped)
	}

	if got := requests.Load(); got != int64(loadResult.TotalSent) {
		t.Errorf("server received %d requests, result reports %d", got, loadResult.TotalSent)
	}

	expectedActualRPS := float64(loadResult.TotalSent) / float64(loadResult.DurationSeconds)

	if math.Abs(loadResult.ActualRPS-expectedActualRPS) > 0.000001 {
		t.Errorf("actual RPS = %f, want %f", loadResult.ActualRPS, expectedActualRPS)
	}
}

func TestHTTPLoadTester_HandleTask_CancelledContextCancelsActiveRequest(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	requestCanceled := make(chan struct{}, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}

		<-r.Context().Done()

		requestCanceled <- struct{}{}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tester := &HTTPLoadTester{}

	task := newHTTPLoadTesterTask(t, server.URL, 1, 5)

	type handleResult struct {
		result ServiceResult
		err    error
	}

	done := make(chan handleResult, 1)

	go func() {
		result, err := tester.HandleTask(ctx, task)
		done <- handleResult{
			result: result,
			err:    err,
		}
	}()

	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP request was not started")
	}

	cancel()

	select {
	case <-requestCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP request was not canceled after parent context cancellation")
	}

	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) {
			t.Errorf("HandleTask() error = %v, want context.Canceled", got.err)
		}

		if got.result.Error != nil {
			t.Errorf("service error = %+v, want nil", got.result.Error)
		}

		loadResult, ok := got.result.Result.(HTTPLoadTesterResult)
		if !ok {
			t.Fatalf("result type = %T, want HTTPLoadTesterResult", got.result.Result)
		}

		if loadResult.TotalSent != 1 {
			t.Errorf("total sent = %d, want 1", loadResult.TotalSent)
		}

		if loadResult.TransportErrors != 1 {
			t.Errorf("transport errors = %d, want 1", loadResult.TransportErrors)
		}

	case <-time.After(3 * time.Second):
		t.Fatal("HandleTask() did not return after context cancellation")
	}
}

func newHTTPLoadTesterTask(t *testing.T, url string, targetRPS int, durationSeconds int) Task {
	t.Helper()

	payload, err := json.Marshal(HTTPLoadTesterPayload{
		URL:             url,
		TargetRPS:       targetRPS,
		DurationSeconds: durationSeconds,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	return Task{
		Payload: payload,
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
