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
