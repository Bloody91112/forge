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

type RenewTaskInput struct {
	TaskID     uuid.UUID
	WorkerID   uuid.UUID `json:"worker_id"`
	LeaseToken uuid.UUID `json:"lease_token"`
}

type CompleteTaskInput struct {
	TaskID     uuid.UUID
	WorkerID   uuid.UUID       `json:"worker_id"`
	LeaseToken uuid.UUID       `json:"lease_token"`
	Result     json.RawMessage `json:"result"`
}

type FailTaskInput struct {
	TaskID     uuid.UUID
	WorkerID   uuid.UUID `json:"worker_id"`
	LeaseToken uuid.UUID `json:"lease_token"`
	Error      struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Retryable *bool `json:"retryable"`
}
