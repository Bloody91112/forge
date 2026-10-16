package task

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

type Task struct {
	ID             uuid.UUID
	ClientID       uuid.UUID
	IdempotencyKey string
	RequestHash    string
	Type           string
	Payload        json.RawMessage
	Status         Status
	Result         json.RawMessage
	ErrorCode      *string
	ErrorMessage   *string
	AttemptCount   int64
	MaxAttempts    int64
	TimeoutSeconds int64
	CreatedAt      time.Time
	AvailableAt    time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
}
