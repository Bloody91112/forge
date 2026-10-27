package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"forge/internal/httpresponse"
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

func (s *FakeTaskService) GetByID(ctx context.Context, taskID uuid.UUID) (task.Task, error) {
	return s.task, s.err
}

func (s *FakeTaskService) Create(ctx context.Context, dto task.CreateTaskInput) (task.Task, error) {
	s.createInput = dto
	return s.task, s.err
}

func TestTaskHandler_CreateTask(t *testing.T) {
	idempotencyKey := "123"

	wantTask := task.Task{
		ID:        uuid.New(),
		Type:      "email-send",
		Status:    task.StatusQueued,
		CreatedAt: time.Now(),
	}
	wantResponse := newCreateTaskResponse(wantTask)

	tests := []struct {
		name         string
		headers      map[string]string
		body         string
		serviceErr   error
		serviceTask  task.Task
		wantResponse CreateTaskResponse
		wantHTTPCode int
		wantErrCode  string
	}{
		{
			name: "invalid content type",
			headers: map[string]string{
				"Content-Type":    "text",
				"Idempotency-Key": idempotencyKey,
			},
			body:         `{"type":"send-email","payload":{"recipient":"user@example.com"}}`,
			wantHTTPCode: http.StatusUnsupportedMediaType,
			wantErrCode:  httpresponse.ErrCodeMediaTypeError,
		},
		{
			name: "missing idempotency key",
			headers: map[string]string{
				"Content-Type": "application/json",
			},
			body:         `{"type":"send-email","payload":{"recipient":"user@example.com"}}`,
			wantHTTPCode: http.StatusBadRequest,
			wantErrCode:  httpresponse.ErrCodeIdempotencyKeyError,
		},
		{
			name: "invalid JSON",
			headers: map[string]string{
				"Content-Type":    "application/json",
				"Idempotency-Key": idempotencyKey,
			},
			body:         `{"type":"send-email","payload":{"recipient":"user@example.com"},}`,
			wantHTTPCode: http.StatusBadRequest,
			wantErrCode:  httpresponse.ErrCodeInvalidJSONPayload,
		},
		{
			name: "multiple JSON values",
			headers: map[string]string{
				"Content-Type":    "application/json",
				"Idempotency-Key": idempotencyKey,
			},
			body:         `{"type":"send-email","payload":{"recipient":"user@example.com"}} {}`,
			wantHTTPCode: http.StatusBadRequest,
			wantErrCode:  httpresponse.ErrCodeInvalidJSONPayload,
		},
		{
			name: "invalid task input",
			headers: map[string]string{
				"Content-Type":    "application/json",
				"Idempotency-Key": idempotencyKey,
			},
			body:         `{"type":"send-email","payload":{"recipient":"user@example.com"}}`,
			serviceErr:   task.ErrInvalidInput,
			wantHTTPCode: http.StatusBadRequest,
			wantErrCode:  httpresponse.ErrCodeInvalidPayload,
		},
		{
			name: "missing client ID",
			headers: map[string]string{
				"Content-Type":    "application/json",
				"Idempotency-Key": idempotencyKey,
			},
			body:         `{"type":"send-email","payload":{"recipient":"user@example.com"}}`,
			serviceErr:   task.ErrMissingClientID,
			wantHTTPCode: http.StatusInternalServerError,
			wantErrCode:  httpresponse.ErrCodeCreateTaskFailed,
		},
		{
			name: "idempotency conflict",
			headers: map[string]string{
				"Content-Type":    "application/json",
				"Idempotency-Key": idempotencyKey,
			},
			body:         `{"type":"send-email","payload":{"recipient":"user@example.com"}}`,
			serviceErr:   task.ErrIdempotencyConflict,
			wantHTTPCode: http.StatusConflict,
			wantErrCode:  httpresponse.ErrCodeIdempotencyConflict,
		},
		{
			name: "unknown service error",
			headers: map[string]string{
				"Content-Type":    "application/json",
				"Idempotency-Key": idempotencyKey,
			},
			body:         `{"type":"send-email","payload":{"recipient":"user@example.com"}}`,
			serviceErr:   errors.New("something went wrong"),
			wantHTTPCode: http.StatusInternalServerError,
			wantErrCode:  httpresponse.ErrCodeCreateTaskFailed,
		},
		{
			name: "success",
			headers: map[string]string{
				"Content-Type":    "application/json",
				"Idempotency-Key": idempotencyKey,
			},
			body:         `{"type":"send-email","payload":{"recipient":"user@example.com"}}`,
			serviceTask:  wantTask,
			wantHTTPCode: http.StatusAccepted,
			wantResponse: wantResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/tasks",
				strings.NewReader(tt.body),
			)

			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			service := &FakeTaskService{
				task: tt.serviceTask,
				err:  tt.serviceErr,
			}
			handler := NewTaskHandler(service)

			mux := http.NewServeMux()
			mux.HandleFunc("POST /api/v1/tasks", handler.CreateTask)

			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			if tt.wantErrCode != "" {
				assertJSONErrorResponse(t, rr, tt.wantHTTPCode, tt.wantErrCode)
				return
			}

			if rr.Code != tt.wantHTTPCode {
				t.Fatalf(
					"HTTP status = %d, want %d; body: %s",
					rr.Code,
					tt.wantHTTPCode,
					rr.Body.String(),
				)
			}

			assertJSONContentType(t, rr)

			if service.createInput.IdempotencyKey != idempotencyKey {
				t.Errorf(
					"service idempotency key = %q, want %q",
					service.createInput.IdempotencyKey,
					idempotencyKey,
				)
			}

			var gotResponse CreateTaskResponse
			if err := json.NewDecoder(rr.Body).Decode(&gotResponse); err != nil {
				t.Fatalf("decode task response: %v", err)
			}

			if diff := cmp.Diff(tt.wantResponse, gotResponse); diff != "" {
				t.Errorf("task response mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTaskHandler_CreateTask_TooLargeRequestBody(t *testing.T) {
	taskInput := task.CreateTaskInput{
		Type: strings.Repeat("A", maxTaskRequestBodySize+1),
	}

	jsonBytes, err := json.Marshal(taskInput)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/tasks",
		bytes.NewReader(jsonBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "123")

	service := &FakeTaskService{}
	handler := NewTaskHandler(service)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tasks", handler.CreateTask)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assertJSONErrorResponse(
		t,
		rr,
		http.StatusRequestEntityTooLarge,
		httpresponse.ErrCodeRequestBodyLimit,
	)
}

func TestTaskHandler_GetTask(t *testing.T) {
	taskID := uuid.New()

	wantTask := task.Task{
		ID:     taskID,
		Type:   "email-send",
		Status: task.StatusQueued,
		Result: []byte(`{"user_id":42}`),
	}
	wantResponse := newGetTaskResponse(wantTask)

	tests := []struct {
		name         string
		pathID       string
		serviceErr   error
		serviceTask  task.Task
		wantResponse *GetTaskResponse
		wantHTTPCode int
		wantErrCode  string
	}{
		{
			name:         "invalid task ID",
			pathID:       "123",
			wantHTTPCode: http.StatusBadRequest,
			wantErrCode:  httpresponse.ErrCodeInvalidTaskID,
		},
		{
			name:         "success",
			pathID:       taskID.String(),
			serviceTask:  wantTask,
			wantResponse: &wantResponse,
			wantHTTPCode: http.StatusOK,
		},
		{
			name:         "task not found",
			pathID:       taskID.String(),
			serviceErr:   task.ErrTaskNotFound,
			wantHTTPCode: http.StatusNotFound,
			wantErrCode:  httpresponse.ErrCodeTaskNotFound,
		},
		{
			name:         "unknown service error",
			pathID:       taskID.String(),
			serviceErr:   errors.New("something went wrong"),
			wantHTTPCode: http.StatusInternalServerError,
			wantErrCode:  httpresponse.ErrCodeGetTaskFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodGet,
				"/api/v1/tasks/"+tt.pathID,
				nil,
			)

			service := &FakeTaskService{
				task: tt.serviceTask,
				err:  tt.serviceErr,
			}
			handler := NewTaskHandler(service)

			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/v1/tasks/{id}", handler.GetTask)

			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			if tt.wantErrCode != "" {
				assertJSONErrorResponse(t, rr, tt.wantHTTPCode, tt.wantErrCode)
				return
			}

			if rr.Code != tt.wantHTTPCode {
				t.Fatalf(
					"HTTP status = %d, want %d; body: %s",
					rr.Code,
					tt.wantHTTPCode,
					rr.Body.String(),
				)
			}

			assertJSONContentType(t, rr)

			var gotResponse GetTaskResponse
			if err := json.NewDecoder(rr.Body).Decode(&gotResponse); err != nil {
				t.Fatalf("decode task response: %v", err)
			}

			if diff := cmp.Diff(*tt.wantResponse, gotResponse); diff != "" {
				t.Errorf("task response mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
