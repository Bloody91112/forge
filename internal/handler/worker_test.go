package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"forge/internal/httpresponse"
	"forge/internal/worker"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
)

type workerServiceStub struct {
	output worker.RegisterWorkerOutput
	err    error
	calls  int
	token  string
	input  worker.RegisterWorkerInput
}

func (s *workerServiceStub) Register(ctx context.Context, bootstrapToken string, input worker.RegisterWorkerInput) (worker.RegisterWorkerOutput, error) {
	s.calls++
	s.token = bootstrapToken
	s.input = input
	return s.output, s.err
}

func workerRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer bootstrap-secret")
	return req
}

func validWorkerJSON() string {
	return `{"worker_id":"` + uuid.NewString() + `","protocol_version":"1","capabilities":["email"]}`
}

func assertWorkerErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()

	if recorder.Code != wantStatus {
		t.Fatalf("HTTP status = %d, want %d; body: %s", recorder.Code, wantStatus, recorder.Body.String())
	}

	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var response httpresponse.ErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}

	if response.Error.Code != wantCode {
		t.Fatalf("error code = %q, want %q", response.Error.Code, wantCode)
	}
}

func TestWorkerHandler_RegisterRejectsInvalidRequests(t *testing.T) {
	oversizedBody := `{"worker_id":"` + uuid.NewString() +
		`","protocol_version":"1","capabilities":["` +
		strings.Repeat("x", 1<<20) + `"]}`

	tests := []struct {
		name        string
		contentType string
		authHeader  string
		body        string
		wantStatus  int
		wantCode    string
	}{
		{
			name:        "unsupported content type",
			contentType: "text/plain",
			authHeader:  "Bearer bootstrap-secret",
			body:        validWorkerJSON(),
			wantStatus:  http.StatusUnsupportedMediaType,
			wantCode:    httpresponse.ErrCodeMediaTypeError,
		},
		{
			name:        "missing authorization",
			contentType: "application/json",
			body:        validWorkerJSON(),
			wantStatus:  http.StatusUnauthorized,
			wantCode:    httpresponse.ErrCodeNotAuthorized,
		},
		{
			name:        "invalid JSON",
			contentType: "application/json",
			authHeader:  "Bearer bootstrap-secret",
			body:        `{"worker_id":`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    httpresponse.ErrCodeInvalidJSONPayload,
		},
		{
			name:        "multiple JSON values",
			contentType: "application/json",
			authHeader:  "Bearer bootstrap-secret",
			body:        validWorkerJSON() + ` {}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    httpresponse.ErrCodeInvalidJSONPayload,
		},
		{
			name:        "request body exceeds limit",
			contentType: "application/json",
			authHeader:  "Bearer bootstrap-secret",
			body:        oversizedBody,
			wantStatus:  http.StatusRequestEntityTooLarge,
			wantCode:    httpresponse.ErrCodeRequestBodyLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &workerServiceStub{}
			handler := NewWorkerHandler(service)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/workers", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			recorder := httptest.NewRecorder()
			handler.Register(recorder, req)

			assertWorkerErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)

			if service.calls != 0 {
				t.Fatalf("service calls = %d, want 0", service.calls)
			}
		})
	}
}

func TestWorkerHandler_RegisterMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		serviceErr error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid input",
			serviceErr: worker.ErrInvalidInput,
			wantStatus: http.StatusBadRequest,
			wantCode:   httpresponse.ErrCodeInvalidPayload,
		},
		{
			name:       "invalid bootstrap token",
			serviceErr: worker.ErrInvalidBootstrapToken,
			wantStatus: http.StatusUnauthorized,
			wantCode:   httpresponse.ErrCodeAuthFailed,
		},
		{
			name:       "unsupported protocol version",
			serviceErr: worker.ErrUnsupportedProtocolVersion,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   httpresponse.ErrUnsupportedProtocolVersion,
		},
		{
			name:       "unexpected service error",
			serviceErr: errors.New("database unavailable"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   httpresponse.ErrCodeRegisterWorkerFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &workerServiceStub{err: tt.serviceErr}
			handler := NewWorkerHandler(service)

			recorder := httptest.NewRecorder()
			handler.Register(recorder, workerRequest(validWorkerJSON()))

			assertWorkerErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)

			if service.calls != 1 {
				t.Fatalf("service calls = %d, want 1", service.calls)
			}

			if service.token != "bootstrap-secret" {
				t.Fatalf("bootstrap token passed to service = %q", service.token)
			}
		})
	}
}

func TestWorkerHandler_RegisterSuccess(t *testing.T) {
	want := worker.RegisterWorkerOutput{
		ID:             uuid.New(),
		Token:          "generated-worker-token",
		LeaseExpiresAt: workerLeaseExpiry(),
	}
	service := &workerServiceStub{output: want}
	handler := NewWorkerHandler(service)

	body := `{"worker_id":"` + want.ID.String() +
		`","protocol_version":"1","capabilities":["email","webhook"]}`

	recorder := httptest.NewRecorder()
	handler.Register(recorder, workerRequest(body))

	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d; body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	if service.calls != 1 {
		t.Fatalf("service calls = %d, want 1", service.calls)
	}

	if service.token != "bootstrap-secret" {
		t.Fatalf("bootstrap token = %q, want bootstrap-secret", service.token)
	}

	if service.input.ID != want.ID {
		t.Fatalf("service input ID = %s, want %s", service.input.ID, want.ID)
	}

	if service.input.ProtocolVersion != worker.RequiredProtocolVersion {
		t.Fatalf(
			"service input protocol = %q, want %q",
			service.input.ProtocolVersion,
			worker.RequiredProtocolVersion,
		)
	}

	var capabilities []string
	if err := json.Unmarshal(service.input.Capabilities, &capabilities); err != nil {
		t.Fatalf("decode capabilities passed to service: %v", err)
	}

	if diff := cmp.Diff([]string{"email", "webhook"}, capabilities); diff != "" {
		t.Fatalf("service capabilities mismatch (-want +got):\n%s", diff)
	}

	var got worker.RegisterWorkerOutput
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode success response: %v", err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("response mismatch (-want +got):\n%s", diff)
	}
}

func workerLeaseExpiry() time.Time {
	return time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
}
