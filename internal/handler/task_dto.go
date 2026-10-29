package handler

import (
	"encoding/json"
	"time"

	"forge/internal/task"

	"github.com/google/uuid"
)

type CreateTaskResponse struct {
	ID        uuid.UUID   `json:"id"`
	Type      string      `json:"type"`
	Status    task.Status `json:"status"`
	CreatedAt time.Time   `json:"created_at"`
}

type GetTaskResponse struct {
	ID           uuid.UUID          `json:"id"`
	Type         string             `json:"type"`
	Status       task.Status        `json:"status"`
	AttemptCount int64              `json:"attempt_count"`
	Result       json.RawMessage    `json:"result"`
	Error        *TaskErrorResponse `json:"error"`
	CreatedAt    time.Time          `json:"created_at"`
	StartedAt    *time.Time         `json:"started_at"`
	FinishedAt   *time.Time         `json:"finished_at"`
}

type ClaimTaskItemResponse struct {
	ID             uuid.UUID       `json:"id"`
	Type           string          `json:"type"`
	Payload        json.RawMessage `json:"payload"`
	AttemptNumber  int64           `json:"attempt_number"`
	TimeoutSeconds int64           `json:"timeout_seconds"`
	LeaseToken     uuid.UUID       `json:"lease_token"`
	LeaseExpiresAt time.Time       `json:"lease_expires_at"`
}

type RenewTaskResponse struct {
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}

type ClaimTaskResponse struct {
	Task ClaimTaskItemResponse `json:"task"`
}

type TaskErrorResponse struct {
	Code    *string `json:"code"`
	Message *string `json:"message"`
}

func newCreateTaskResponse(t task.Task) CreateTaskResponse {
	return CreateTaskResponse{
		ID:        t.ID,
		Type:      t.Type,
		Status:    t.Status,
		CreatedAt: t.CreatedAt,
	}
}

func newClaimTaskResponse(t task.Task) ClaimTaskResponse {
	return ClaimTaskResponse{
		Task: ClaimTaskItemResponse{
			ID:             t.ID,
			Type:           t.Type,
			Payload:        t.Payload,
			AttemptNumber:  t.AttemptCount,
			TimeoutSeconds: t.TimeoutSeconds,
			LeaseToken:     t.LeaseToken,
			LeaseExpiresAt: *t.LeaseExpiresAt,
		},
	}
}

func newGetTaskResponse(t task.Task) GetTaskResponse {
	var taskError *TaskErrorResponse

	if t.ErrorCode != nil || t.ErrorMessage != nil {
		taskError = &TaskErrorResponse{
			Code:    t.ErrorCode,
			Message: t.ErrorMessage,
		}
	}

	return GetTaskResponse{
		ID:           t.ID,
		Type:         t.Type,
		Status:       t.Status,
		AttemptCount: t.AttemptCount,
		Result:       t.Result,
		Error:        taskError,
		CreatedAt:    t.CreatedAt,
		StartedAt:    t.StartedAt,
		FinishedAt:   t.FinishedAt,
	}
}
