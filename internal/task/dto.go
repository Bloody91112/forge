package task

import (
	"encoding/json"

	"github.com/google/uuid"
)

type CreateTaskInput struct {
	Type           string          `json:"type"`
	Payload        json.RawMessage `json:"payload"`
	TimeoutSeconds *int64          `json:"timeout_seconds"`
	MaxAttempts    *int64          `json:"max_attempts"`
	IdempotencyKey string
}

type ClaimTaskInput struct {
	WorkerID uuid.UUID `json:"worker_id"`
}
