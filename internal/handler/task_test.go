package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"forge/internal/task"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
)

type FakeTaskService struct {
	task        task.Task
	err         error
	createInput task.CreateTaskInput
}

func (s *FakeTaskService) GetByID(ctx context.Context, id uuid.UUID) (task.Task, error) {
	return s.task, s.err
}

func (s *FakeTaskService) Create(ctx context.Context, dto task.CreateTaskInput) (task.Task, error) {
	s.createInput = dto
	return s.task, s.err
}

func TestTaskHandler_CreateTask(t *testing.T) {
	wantTask := task.Task{
		ID:        uuid.New(),
		Type:      "email-send",
		Status:    task.StatusQueued,
		CreatedAt: time.Now(),
	}

	wantTaskResponse := newCreateTaskResponse(wantTask)

	tests := []struct {
		name         string
		headers      map[string]string
		body         string
		serviceErr   error
		serviceTask  task.Task
		wantResponse CreateTaskResponse
		wantHttpCode int
		wantErrCode  string
	}{
		{
			name: "not json content type provided response",
			headers: map[string]string{
				"Content-type":    "text",
				"Idempotency-Key": "123",
			},
			body:         `{"type": "send-email", "payload": {"recipient": "user@example.com"}}`,
			wantHttpCode: http.StatusBadRequest,
			wantErrCode:  "media_type_error",
		},
		{
			name: "no idempotency key error response",
			headers: map[string]string{
				"Content-type": "application/json",
			},
			body:         `{"type": "send-email", "payload": {"recipient": "user@example.com"}}`,
			wantHttpCode: http.StatusBadRequest,
			wantErrCode:  "idempotency_key_error",
		},
		{
			name: "not valid json payload error response",
			headers: map[string]string{
				"Content-type":    "application/json",
				"Idempotency-Key": "123",
			},
			body:         `{"type": "send-email", "payload": {"recipient": "user@example.com"},}`,
			wantHttpCode: http.StatusBadRequest,
			wantErrCode:  "invalid_json_payload",
		},
		{
			name: "second json payload error response",
			headers: map[string]string{
				"Content-type":    "application/json",
				"Idempotency-Key": "123",
			},
			body:         `{"type": "send-email", "payload": {"recipient": "user@example.com"} {}}`,
			wantHttpCode: http.StatusBadRequest,
			wantErrCode:  "invalid_json_payload",
		},
		{
			name: "failed payload param check in service response",
			headers: map[string]string{
				"Content-type":    "application/json",
				"Idempotency-Key": "123",
			},
			body:         `{"type": "send-email", "payload": {"recipient": "user@example.com"}}`,
			serviceErr:   task.ErrInvalidInput,
			wantHttpCode: http.StatusBadRequest,
			wantErrCode:  "invalid_payload_params",
		},
		{
			name: "no client id error response",
			headers: map[string]string{
				"Content-type":    "application/json",
				"Idempotency-Key": "123",
			},
			body:         `{"type": "send-email", "payload": {"recipient": "user@example.com"}}`,
			serviceErr:   task.ErrMissingClientID,
			wantHttpCode: http.StatusInternalServerError,
			wantErrCode:  "create_task_error",
		},
		{
			name: "idempotency conflict error response",
			headers: map[string]string{
				"Content-type":    "application/json",
				"Idempotency-Key": "123",
			},
			body:         `{"type": "send-email", "payload": {"recipient": "user@example.com"}}`,
			serviceErr:   task.ErrIdempotencyConflict,
			wantHttpCode: http.StatusConflict,
			wantErrCode:  "idempotency_key_conflict",
		},
		{
			name: "unknown error response",
			headers: map[string]string{
				"Content-type":    "application/json",
				"Idempotency-Key": "123",
			},
			body:         `{"type": "send-email", "payload": {"recipient": "user@example.com"}}`,
			serviceErr:   errors.New("something went wrong"),
			wantHttpCode: http.StatusInternalServerError,
			wantErrCode:  "create_task_error",
		},
		{
			name: "happy path response",
			headers: map[string]string{
				"Content-type":    "application/json",
				"Idempotency-Key": "123",
			},
			body:         `{"type": "send-email", "payload": {"recipient": "user@example.com"}}`,
			serviceTask:  wantTask,
			wantHttpCode: http.StatusAccepted,
			wantResponse: wantTaskResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(tt.body))
			rr := httptest.NewRecorder()

			for headerKey, headerValue := range tt.headers {
				req.Header.Set(headerKey, headerValue)
			}

			service := FakeTaskService{
				task: tt.serviceTask,
				err:  tt.serviceErr,
			}

			handler := NewTaskHandler(&service)

			mux := http.NewServeMux()
			mux.HandleFunc("POST /api/v1/tasks", handler.CreateTask)
			mux.ServeHTTP(rr, req)

			if rr.Code != tt.wantHttpCode {
				t.Errorf("expected http code: %d, got: %d", tt.wantHttpCode, rr.Code)
			}

			if tt.wantErrCode != "" {
				var responseErr ErrorResponse
				if err := json.NewDecoder(rr.Body).Decode(&responseErr); err != nil {
					t.Fatalf("failed to decode err: %v", err)
				}

				if responseErr.Error.Code != tt.wantErrCode {
					t.Fatalf("expected error code: %s, got: %s", tt.wantErrCode, responseErr.Error.Code)
				}
			}

			if tt.wantResponse != (CreateTaskResponse{}) {
				if service.createInput.IdempotencyKey != "123" {
					t.Errorf("expected service idempotency key: %q, got: %q", "123", service.createInput.IdempotencyKey)
				}

				var taskResponse CreateTaskResponse
				if err := json.NewDecoder(rr.Body).Decode(&taskResponse); err != nil {
					t.Fatalf("failed to decode task response: %v", err)
				}

				if diff := cmp.Diff(tt.wantResponse, taskResponse); diff != "" {
					t.Errorf("task response mismatch (-want +got):\n%s", diff)
				}
			}

			if responseContentType := rr.Header().Get("Content-Type"); responseContentType != "application/json" {
				t.Errorf("expected json response content type, got: %s", responseContentType)
			}

		})
	}
}

func TestTaskHandler_CreateTask_TooLargeRequestBody(t *testing.T) {
	var taskInput task.CreateTaskInput

	oneMBPlusString := strings.Repeat("A", (1<<20)+1)
	taskInput.Type = oneMBPlusString

	jsonBytes, err := json.Marshal(taskInput)
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(jsonBytes))
	rr := httptest.NewRecorder()

	req.Header.Set("Content-type", "application/json")
	req.Header.Set("Idempotency-Key", "123")

	service := FakeTaskService{}

	handler := NewTaskHandler(&service)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tasks", handler.CreateTask)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected http code: %d, got: %d", http.StatusRequestEntityTooLarge, rr.Code)
	}

	var responseErr ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&responseErr); err != nil {
		t.Fatalf("failed to decode err: %v", err)
	}

	if responseErr.Error.Code != "request_body_limit_error" {
		t.Fatalf("expected error code: %s, got: %s", "request_body_limit_error", responseErr.Error.Code)
	}

}
