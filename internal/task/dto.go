package task

import (
	"encoding/json"
)

type CreateTaskInput struct {
	Type           string
	Payload        json.RawMessage
	TimeoutSeconds *int64
	MaxAttempts    *int64
	IdempotencyKey string
}
