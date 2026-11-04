package forgeapi

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type RegisterRequest struct {
	WorkerID        uuid.UUID `json:"worker_id"`
	ProtocolVersion string    `json:"protocol_version"`
	Capabilities    []string  `json:"capabilities"`
}

type RegisterResponse struct {
	WorkerID       uuid.UUID `json:"worker_id"`
	WorkerToken    string    `json:"worker_token"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}

type HeartbeatRequest struct {
	WorkerID uuid.UUID `json:"worker_id"`
}

type HeartbeatResponse struct {
	WorkerID       uuid.UUID `json:"worker_id"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}

type ClaimTaskRequest struct {
	WorkerID uuid.UUID `json:"worker_id"`
}

type ClaimTaskResponse struct {
	Task ClaimedTask `json:"task"`
}

type ClaimedTask struct {
	ID             uuid.UUID       `json:"id"`
	Type           string          `json:"type"`
	Payload        json.RawMessage `json:"payload"`
	AttemptNumber  int64           `json:"attempt_number"`
	TimeoutSeconds int64           `json:"timeout_seconds"`
	LeaseToken     string          `json:"lease_token"`
	LeaseExpiresAt time.Time       `json:"lease_expires_at"`
}

type RenewTaskRequest struct {
	WorkerID   uuid.UUID `json:"worker_id"`
	LeaseToken string    `json:"lease_token"`
}

type RenewTaskResponse struct {
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}

type CompleteTaskRequest struct {
	WorkerID   uuid.UUID       `json:"worker_id"`
	LeaseToken string          `json:"lease_token"`
	Result     json.RawMessage `json:"result"`
}

type FailTaskRequest struct {
	WorkerID   uuid.UUID `json:"worker_id"`
	LeaseToken string    `json:"lease_token"`
	Error      TaskError `json:"error"`
	Retryable  bool      `json:"retryable"`
}

type TaskError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type RemoteError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *RemoteError) Error() string {
	return fmt.Sprintf("Forge returned %d: %s: %s", e.StatusCode, e.Code, e.Message)
}

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}
