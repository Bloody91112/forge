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

	"github.com/google/uuid"
)

type heartbeatServiceStub struct {
	output worker.HeartbeatResponseDTO
	err    error
	calls  int
	input  worker.HeartbeatRequestDTO
}

func (s *heartbeatServiceStub) Register(
	context.Context,
	string,
	worker.RegisterRequestDTO,
) (worker.RegisterResponseDTO, error) {
	panic("Register must not be called by heartbeat handler")
}

func (s *heartbeatServiceStub) ExtendLease(
	_ context.Context,
	input worker.HeartbeatRequestDTO,
) (worker.HeartbeatResponseDTO, error) {
	s.calls++
	s.input = input
	return s.output, s.err
}

func TestWorkerHandler_HeartbeatSuccess(t *testing.T) {
	workerID := uuid.New()
	expiry := time.Date(2026, time.October, 26, 12, 0, 30, 0, time.UTC)

	want := worker.HeartbeatResponseDTO{
		WorkerID:       workerID,
		LeaseExpiresAt: expiry,
	}
	service := &heartbeatServiceStub{output: want}
	handler := NewWorkerHandler(service)

	req := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/workers/heartbeat",
		strings.NewReader(`{"worker_id":"`+workerID.String()+`"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Heartbeat(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d; body: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}

	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	if service.calls != 1 {
		t.Fatalf("service calls = %d, want 1", service.calls)
	}

	if service.input.WorkerID != workerID {
		t.Fatalf("service worker ID = %s, want %s", service.input.WorkerID, workerID)
	}

	var got worker.HeartbeatResponseDTO
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode heartbeat response: %v", err)
	}

	if got.WorkerID != want.WorkerID {
		t.Fatalf("response worker ID = %s, want %s", got.WorkerID, want.WorkerID)
	}

	if !got.LeaseExpiresAt.Equal(want.LeaseExpiresAt) {
		t.Fatalf(
			"response lease expiry = %v, want %v",
			got.LeaseExpiresAt,
			want.LeaseExpiresAt,
		)
	}
}

func TestWorkerHandler_HeartbeatMapsServiceErrors(t *testing.T) {
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
			name:       "unexpected service error",
			serviceErr: errors.New("database unavailable"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   httpresponse.ErrCodeHeartbeatWorkerFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &heartbeatServiceStub{err: tt.serviceErr}
			handler := NewWorkerHandler(service)
			workerID := uuid.New()

			req := httptest.NewRequest(
				http.MethodPost,
				"/internal/v1/workers/heartbeat",
				strings.NewReader(`{"worker_id":"`+workerID.String()+`"}`),
			)
			req.Header.Set("Content-Type", "application/json")

			recorder := httptest.NewRecorder()
			handler.Heartbeat(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf(
					"HTTP status = %d, want %d; body: %s",
					recorder.Code,
					tt.wantStatus,
					recorder.Body.String(),
				)
			}

			var got httpresponse.ErrorResponse
			if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
				t.Fatalf("decode error response: %v", err)
			}

			if got.Error.Code != tt.wantCode {
				t.Fatalf("error code = %q, want %q", got.Error.Code, tt.wantCode)
			}

			if service.calls != 1 {
				t.Fatalf("service calls = %d, want 1", service.calls)
			}
		})
	}
}
