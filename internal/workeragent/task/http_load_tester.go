package task

import (
	"context"
	"encoding/json"
	"fmt"
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
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	DurationSeconds int     `json:"duration_seconds"`
}

type SecondResult struct {
	total     int
	sent      int
	completed int
	errors    int
	dropped   int
	latencyMs int64
}

type RequestResult struct {
	createdAt   *time.Time
	completedAt *time.Time
	sent        bool
	code        *int
	err         error
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

	deadline := time.Now().Add(time.Duration(payload.DurationSeconds) * time.Second)
	deadlineCtx, callDeadline := context.WithDeadline(ctx, deadline)
	defer callDeadline()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	results := make(chan SecondResult)
	defer close(results)

	for {
		select {
		case <-ctx.Done():
			return ServiceResult{}, ctx.Err()
		case <-deadlineCtx.Done():
			close(results)

			return ServiceResult{}, nil
		case <-ticker.C:
			// makeOneSecondLoad
		}

	}

}

func (t *HTTPLoadTester) makeOneSecondLoad(ctx context.Context, resultsCh chan<- SecondResult, targetRPS int, URL string) {
	oneSecCtx, oneSecCancel := context.WithDeadline(ctx, time.Now().Add(time.Second))
	defer oneSecCancel()

	var wg sync.WaitGroup

	requestsCh := make(chan RequestResult, targetRPS)

	for range targetRPS {
		wg.Go(func() {
			createdAt := time.Now()

			if oneSecCtx.Err() != nil {
				requestsCh <- RequestResult{
					createdAt: &createdAt,
					err:       oneSecCtx.Err(),
					sent:      false,
				}
				return
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, URL, nil)

			if err != nil {
				requestsCh <- RequestResult{
					createdAt: &createdAt,
					err:       err,
					sent:      false,
				}
				return
			}

			resp, err := t.client.Do(req)

			var completedAt *time.Time
			var code *int

			if resp != nil {
				defer resp.Body.Close()

				now := time.Now()
				completedAt = &now

				statusCode := resp.StatusCode
				code = &statusCode
			}

			requestsCh <- RequestResult{
				createdAt:   &createdAt,
				completedAt: completedAt,
				code:        code,
				err:         err,
				sent:        true,
			}
		})
	}

	wg.Wait()

	close(requestsCh)

	var sr SecondResult

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

		if rr.completedAt != nil {
			sr.completed++
			sr.latencyMs += rr.completedAt.Sub(*rr.createdAt).Milliseconds()
		}
	}

	resultsCh <- sr
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
