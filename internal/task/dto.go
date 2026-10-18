package task

import (
	"encoding/json"
)

type CreateTaskInput struct {
	Type           string          `json:"type"`
	Payload        json.RawMessage `json:"payload"`
	TimeoutSeconds *int64          `json:"timeout_seconds"`
	MaxAttempts    *int64          `json:"max_attempts"`
	IdempotencyKey string          `json:"idempotency_key"`
}
