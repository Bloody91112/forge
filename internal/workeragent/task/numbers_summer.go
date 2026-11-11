package task

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type NumbersSummer struct{}

type ExpectedPayload struct {
	Number1 *int `json:"number_1"`
	Number2 *int `json:"number_2"`
}

type numbersSumResult struct {
	Sum int `json:"sum"`
}

func (ns *NumbersSummer) HandleTask(ctx context.Context, task Task) (ServiceResult, error) {
	var payload ExpectedPayload

	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return invalidPayloadResult(fmt.Sprintf("invalid payload: %v", err)), nil
	}

	if payload.Number1 == nil || payload.Number2 == nil {
		return invalidPayloadResult("number_1 and number_2 are required"), nil
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ServiceResult{}, ctx.Err()

	case <-timer.C:
		return ServiceResult{
			Result: numbersSumResult{
				Sum: *payload.Number1 + *payload.Number2,
			},
		}, nil
	}
}

func invalidPayloadResult(message string) ServiceResult {
	return ServiceResult{
		Error: &ServiceError{
			Code:      ErrorCodeTaskInvalidPayload,
			Message:   message,
			Retryable: false,
		},
	}
}
