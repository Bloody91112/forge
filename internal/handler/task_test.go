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
	task          task.Task
	err           error
	createInput   task.CreateTaskInput
	createCreated bool

	claimTask  task.Task
	claimErr   error
	claimInput task.ClaimTaskInput

	renewLeaseExpiresAt time.Time
	renewErr            error
	renewInput          task.RenewTaskInput

	completeErr   error
	completeInput task.CompleteTaskInput

	failErr   error
	failInput task.FailTaskInput
}

func (s *FakeTaskService) GetByID(ctx context.Context, taskID uuid.UUID) (task.Task, error) {
	return s.task, s.err
}

func (s *FakeTaskService) Create(ctx context.Context, dto task.CreateTaskInput) (task.CreateResult, error) {
	s.createInput = dto

	return task.CreateResult{
		Task:    s.task,
		Created: s.createCreated,
	}, s.err
}

func (s *FakeTaskService) Claim(ctx context.Context, dto task.ClaimTaskInput) (task.Task, error) {
	s.claimInput = dto
	return s.claimTask, s.claimErr
}

func (s *FakeTaskService) Renew(ctx context.Context, dto task.RenewTaskInput) (time.Time, error) {
	s.renewInput = dto
	return s.renewLeaseExpiresAt, s.renewErr
}

func (s *FakeTaskService) Complete(ctx context.Context, dto task.CompleteTaskInput) error {
	s.completeInput = dto
	return s.completeErr
}

func (s *FakeTaskService) Fail(ctx context.Context, dto task.FailTaskInput) error {
	s.failInput = dto
	return s.failErr
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
		name           string
		headers        map[string]string
		body           string
		serviceErr     error
		serviceTask    task.Task
		wantResponse   CreateTaskResponse
		wantHTTPCode   int
		wantErrCode    string
		serviceCreated bool
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
			wantHTTPCode: http.StatusUnprocessableEntity,
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
			body:           `{"type":"send-email","payload":{"recipient":"user@example.com"}}`,
			serviceTask:    wantTask,
			wantHTTPCode:   http.StatusAccepted,
			wantResponse:   wantResponse,
			serviceCreated: true,
		},
		{
			name: "no active workers",
			headers: map[string]string{
				"Content-Type":    "application/json",
				"Idempotency-Key": idempotencyKey,
			},
			body:         `{"type":"send-email","payload":{"recipient":"user@example.com"}}`,
			serviceErr:   task.ErrNoActiveWorkers,
			wantHTTPCode: http.StatusServiceUnavailable,
			wantErrCode:  httpresponse.ErrCodeNoActiveWorkers,
		},
		{
			name: "task type not supported",
			headers: map[string]string{
				"Content-Type":    "application/json",
				"Idempotency-Key": idempotencyKey,
			},
			body:         `{"type":"send-email","payload":{"recipient":"user@example.com"}}`,
			serviceErr:   task.ErrTaskTypeNotSupported,
			wantHTTPCode: http.StatusUnprocessableEntity,
			wantErrCode:  httpresponse.ErrCodeTaskTypeNotSupported,
		},
		{
			name: "invalid request structure",
			headers: map[string]string{
				"Content-Type":    "application/json",
				"Idempotency-Key": idempotencyKey,
			},
			body:         `{"type":"send-email","payload":{}}`,
			serviceErr:   task.ErrInvalidRequestStructure,
			wantHTTPCode: http.StatusBadRequest,
			wantErrCode:  httpresponse.ErrCodeInvalidPayload,
		},
		{
			name: "payload too large",
			headers: map[string]string{
				"Content-Type":    "application/json",
				"Idempotency-Key": idempotencyKey,
			},
			body:         `{"type":"send-email","payload":{}}`,
			serviceErr:   task.ErrPayloadTooLarge,
			wantHTTPCode: http.StatusRequestEntityTooLarge,
			wantErrCode:  httpresponse.ErrCodePayloadTooLarge,
		},
		{
			name: "existing idempotent task",
			headers: map[string]string{
				"Content-Type":    "application/json",
				"Idempotency-Key": idempotencyKey,
			},
			body:           `{"type":"send-email","payload":{"recipient":"user@example.com"}}`,
			serviceTask:    wantTask,
			serviceCreated: false,
			wantHTTPCode:   http.StatusOK,
			wantResponse:   wantResponse,
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
				task:          tt.serviceTask,
				err:           tt.serviceErr,
				createCreated: tt.serviceCreated,
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

func TestTaskHandler_ClaimTask(t *testing.T) {
	workerID := uuid.New()
	taskID := uuid.New()
	leaseToken := uuid.New()
	leaseExpiresAt := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

	claimedTask := task.Task{
		ID:             taskID,
		Type:           "email-send",
		Payload:        json.RawMessage(`{"user_id":42}`),
		AttemptCount:   1,
		TimeoutSeconds: 22,
		LeaseToken:     leaseToken,
		LeaseExpiresAt: &leaseExpiresAt,
	}

	tests := []struct {
		name         string
		body         string
		serviceTask  task.Task
		serviceErr   error
		wantHTTPCode int
		wantErrCode  string
	}{
		{
			name:         "invalid content type",
			body:         `{"worker_id":"` + workerID.String() + `"}`,
			wantHTTPCode: http.StatusUnsupportedMediaType,
			wantErrCode:  httpresponse.ErrCodeMediaTypeError,
		},
		{
			name:         "invalid JSON",
			body:         `{"worker_id":`,
			wantHTTPCode: http.StatusBadRequest,
			wantErrCode:  httpresponse.ErrCodeInvalidJSONPayload,
		},
		{
			name:         "invalid worker ID",
			body:         `{}`,
			serviceErr:   task.ErrInvalidInput,
			wantHTTPCode: http.StatusBadRequest,
			wantErrCode:  httpresponse.ErrCodeInvalidPayload,
		},
		{
			name:         "no available tasks",
			body:         `{"worker_id":"` + workerID.String() + `"}`,
			serviceErr:   task.ErrNoAvailableTasks,
			wantHTTPCode: http.StatusNoContent,
		},
		{
			name:         "unknown service error",
			body:         `{"worker_id":"` + workerID.String() + `"}`,
			serviceErr:   errors.New("database unavailable"),
			wantHTTPCode: http.StatusInternalServerError,
			wantErrCode:  httpresponse.ErrCodeFailedToClaimTask,
		},
		{
			name:         "success",
			body:         `{"worker_id":"` + workerID.String() + `"}`,
			serviceTask:  claimedTask,
			wantHTTPCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodPost,
				"/internal/v1/tasks/claim",
				strings.NewReader(tt.body),
			)

			if tt.name == "invalid content type" {
				req.Header.Set("Content-Type", "text/plain")
			} else {
				req.Header.Set("Content-Type", "application/json")
			}

			service := &FakeTaskService{
				claimTask: tt.serviceTask,
				claimErr:  tt.serviceErr,
			}
			handler := NewTaskHandler(service)

			rr := httptest.NewRecorder()
			handler.ClaimTask(rr, req)

			if rr.Code != tt.wantHTTPCode {
				t.Fatalf(
					"HTTP status = %d, want %d; body: %s",
					rr.Code,
					tt.wantHTTPCode,
					rr.Body.String(),
				)
			}

			if tt.wantErrCode != "" {
				assertJSONErrorResponse(t, rr, tt.wantHTTPCode, tt.wantErrCode)
				return
			}

			if tt.wantHTTPCode == http.StatusNoContent {
				if rr.Body.Len() != 0 {
					t.Errorf("204 response body = %q, want empty body", rr.Body.String())
				}
				return
			}

			assertJSONContentType(t, rr)

			if service.claimInput.WorkerID != workerID {
				t.Errorf(
					"service worker ID = %s, want %s",
					service.claimInput.WorkerID,
					workerID,
				)
			}

			var got ClaimTaskResponse
			if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
				t.Fatalf("decode claim response: %v", err)
			}

			want := newClaimTaskResponse(claimedTask)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("claim response mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTaskHandler_CompleteTask(t *testing.T) {
	taskID := uuid.New()
	workerID := uuid.New()
	leaseToken := uuid.New()

	tests := []struct {
		name        string
		body        string
		serviceErr  error
		wantStatus  int
		wantErrCode string
	}{
		{
			name:       "success",
			body:       `{"worker_id":"` + workerID.String() + `","lease_token":"` + leaseToken.String() + `","result":{"ok":true}}`,
			wantStatus: http.StatusOK,
		},
		{
			name:        "lease lost",
			body:        `{"worker_id":"` + workerID.String() + `","lease_token":"` + leaseToken.String() + `","result":{"ok":true}}`,
			serviceErr:  task.ErrTaskLeaseLost,
			wantStatus:  http.StatusConflict,
			wantErrCode: httpresponse.ErrCodeTaskLeaseLost,
		},
		{
			name:        "result too large",
			body:        `{"worker_id":"` + workerID.String() + `","lease_token":"` + leaseToken.String() + `","result":{"ok":true}}`,
			serviceErr:  task.ErrPayloadTooLarge,
			wantStatus:  http.StatusRequestEntityTooLarge,
			wantErrCode: httpresponse.ErrCodePayloadTooLarge,
		},
		{
			name:        "invalid result structure",
			body:        `{"worker_id":"` + workerID.String() + `","lease_token":"` + leaseToken.String() + `","result":{"ok":true}}`,
			serviceErr:  task.ErrInvalidRequestStructure,
			wantStatus:  http.StatusBadRequest,
			wantErrCode: httpresponse.ErrCodeInvalidPayload,
		},
		{
			name:        "invalid input",
			body:        `{"worker_id":"` + workerID.String() + `","lease_token":"` + leaseToken.String() + `","result":{"ok":true}}`,
			serviceErr:  task.ErrInvalidInput,
			wantStatus:  http.StatusUnprocessableEntity,
			wantErrCode: httpresponse.ErrCodeInvalidPayload,
		},
		{
			name:        "internal error",
			body:        `{"worker_id":"` + workerID.String() + `","lease_token":"` + leaseToken.String() + `","result":{"ok":true}}`,
			serviceErr:  errors.New("database unavailable"),
			wantStatus:  http.StatusInternalServerError,
			wantErrCode: httpresponse.ErrCodeFailedToCompleteTask,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/internal/v1/tasks/"+taskID.String()+"/complete", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			service := &FakeTaskService{completeErr: tt.serviceErr}
			handler := NewTaskHandler(service)

			mux := http.NewServeMux()
			mux.HandleFunc("POST /internal/v1/tasks/{id}/complete", handler.CompleteTask)

			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			if tt.wantErrCode != "" {
				assertJSONErrorResponse(t, rr, tt.wantStatus, tt.wantErrCode)
				return
			}

			if rr.Code != tt.wantStatus {
				t.Fatalf("HTTP status = %d, want %d; body: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}

			if service.completeInput.TaskID != taskID {
				t.Errorf("service task ID = %s, want %s", service.completeInput.TaskID, taskID)
			}
			if service.completeInput.WorkerID != workerID {
				t.Errorf("service worker ID = %s, want %s", service.completeInput.WorkerID, workerID)
			}
			if service.completeInput.LeaseToken != leaseToken {
				t.Errorf("service lease token = %s, want %s", service.completeInput.LeaseToken, leaseToken)
			}
		})
	}
}
