package task

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type HTTPLoadTesterPayload struct {
	URL             string `json:"url"`
	TargetRPS       int    `json:"target_rps"`
	DurationSeconds int    `json:"duration_seconds"`
}

type HTTPLoadTesterResult struct {
	TargetRPS       int     `json:"target_rps"`
	ActualRPS       float64 `json:"actual_rps"`
	TotalSent       int     `json:"total_sent"`
	Completed       int     `json:"completed"`
	TransportErrors int     `json:"transport_errors"`
	Dropped         int     `json:"dropped"`
	LatencyTotalMs  float64 `json:"latency_total_ms"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	DurationSeconds int     `json:"duration_seconds"`
}

type SecondResult struct {
	total        int
	sent         int
	completed    int
	errors       int
	dropped      int
	latencyTotal time.Duration
	latencyMin   time.Duration
	latencyMax   time.Duration
	statusCodes  map[int]int
}

type RequestResult struct {
	sent       bool
	startedAt  time.Time
	finishedAt time.Time
	statusCode int
	err        error
}

type HTTPLoadTester struct {
	client *http.Client
}

func (t *HTTPLoadTester) HandleTask(ctx context.Context, task Task) (ServiceResult, error) {
	var payload HTTPLoadTesterPayload

	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return invalidPayloadResult(fmt.Sprintf("invalid payload: %v", err)), nil
	}

	if !isValidHTTPURL(payload.URL) {
		return invalidPayloadResult(fmt.Sprintf("url is not valid: %s", payload.URL)), nil
	}

	if payload.TargetRPS <= 0 {
		return invalidPayloadResult(fmt.Sprintf("target_rps should be positive number, got: %d", payload.TargetRPS)), nil
	}

	if payload.DurationSeconds <= 0 {
		return invalidPayloadResult(fmt.Sprintf("duration_seconds should be positive number, got: %d", payload.DurationSeconds)), nil
	}

	t.client = &http.Client{
		Timeout: 10 * time.Second,
	}

	deadline := time.Now().Add(time.Duration(payload.DurationSeconds) * time.Second)
	deadlineCtx, callDeadline := context.WithDeadline(ctx, deadline)
	defer callDeadline()

	resultsCh := make(chan SecondResult)
	collectedTotalResult := make(chan HTTPLoadTesterResult, 1)

	go func() {
		collectedTotalResult <- t.collectResults(payload.TargetRPS, payload.DurationSeconds, resultsCh)
	}()

	var loadsWG sync.WaitGroup
	var err error

	loadsWG.Go(func() {
		t.makeOneSecondLoad(ctx, resultsCh, payload.TargetRPS, payload.URL)
	})

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

loop:
	for {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			break loop
		case <-deadlineCtx.Done():
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			break loop
		case <-ticker.C:
			if ctx.Err() != nil {
				err = ctx.Err()
				break loop
			}

			if deadlineCtx.Err() != nil {
				break loop
			}

			loadsWG.Go(func() {
				t.makeOneSecondLoad(ctx, resultsCh, payload.TargetRPS, payload.URL)
			})
		}
	}

	loadsWG.Wait()
	close(resultsCh)

	totalResult := <-collectedTotalResult

	return ServiceResult{Result: totalResult}, err
}

func (t *HTTPLoadTester) makeOneSecondLoad(ctx context.Context, resultsCh chan<- SecondResult, targetRPS int, URL string) {
	oneSecCtx, oneSecCancel := context.WithDeadline(ctx, time.Now().Add(time.Second))
	defer oneSecCancel()

	var wg sync.WaitGroup

	requestsCh := make(chan RequestResult, targetRPS)

	for range targetRPS {
		wg.Go(func() {
			if err := oneSecCtx.Err(); err != nil {
				requestsCh <- RequestResult{err: err}
				return
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, URL, nil)

			if err != nil {
				requestsCh <- RequestResult{err: err}
				return
			}

			startedAt := time.Now()
			resp, requestErr := t.client.Do(req)

			result := RequestResult{
				sent:      true,
				startedAt: startedAt,
				err:       requestErr,
			}

			if resp != nil {
				result.statusCode = resp.StatusCode

				_, bodyErr := io.Copy(io.Discard, resp.Body)
				resp.Body.Close()

				if result.err == nil {
					result.err = bodyErr
				}
			}

			result.finishedAt = time.Now()

			requestsCh <- result
		})
	}

	wg.Wait()
	close(requestsCh)

	sr := SecondResult{statusCodes: make(map[int]int)}

	for rr := range requestsCh {
		sr.total++

		if !rr.sent {
			sr.dropped++
			continue
		}

		sr.sent++

		if rr.err != nil {
			sr.errors++
			continue
		}

		sr.completed++
		sr.statusCodes[rr.statusCode]++

		latency := rr.finishedAt.Sub(rr.startedAt)
		sr.latencyTotal += latency

		if sr.completed == 1 || latency < sr.latencyMin {
			sr.latencyMin = latency
		}

		if latency > sr.latencyMax {
			sr.latencyMax = latency
		}
	}

	resultsCh <- sr
}

func (t *HTTPLoadTester) collectResults(targetRPS int, durationSeconds int, resultCh <-chan SecondResult) HTTPLoadTesterResult {
	total := HTTPLoadTesterResult{
		TargetRPS:       targetRPS,
		DurationSeconds: durationSeconds,
	}

	for sr := range resultCh {
		total.TotalSent += sr.sent
		total.Completed += sr.completed
		total.TransportErrors += sr.errors
		total.Dropped += sr.dropped
		total.LatencyTotalMs += float64(sr.latencyTotal) / float64(time.Millisecond)

	}

	if durationSeconds > 0 {
		total.ActualRPS = float64(total.TotalSent) / float64(durationSeconds)
	}

	if total.Completed > 0 {
		total.AvgLatencyMs = total.LatencyTotalMs / float64(total.Completed)
	}

	return total
}

func isValidHTTPURL(rawURL string) bool {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return false
	}

	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}

	return true
}
