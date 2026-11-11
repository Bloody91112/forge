package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type NumbersSummer struct{}

type ExpectedPayload struct {
	Number1 int `json:"number_1"`
	Number2 int `json:"number_2"`
}

func (ns *NumbersSummer) HandleTask(ctx context.Context, task Task) (ServiceResult, error) {
	var payload ExpectedPayload

	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return ServiceResult{}, fmt.Errorf("parse task payload: %w", err)
	}

	sumResultCh := make(chan int)

	go func() {
		time.Sleep(5 * time.Second)
		sumResultCh <- payload.Number1 + payload.Number2
	}()

	select {
	case <-ctx.Done():
		err := ctx.Err()
		var serviceErr *ServiceError
		if errors.Is(err, context.Canceled) {
			serviceErr = &ServiceError{Code: "ctx_canceled", Message: "Context canceled", Retryable: false}
		} else if errors.Is(err, context.DeadlineExceeded) {
			serviceErr = &ServiceError{Code: "ctx_timeout", Message: "Context timeout", Retryable: false}
		} else {
			serviceErr = &ServiceError{Code: "unknown_context_err", Message: "Unknown context error", Retryable: false}
		}

		return ServiceResult{Error: serviceErr, Result: nil}, nil
	case sumResult := <-sumResultCh:
		return ServiceResult{Error: nil, Result: sumResult}, nil
	}

}
