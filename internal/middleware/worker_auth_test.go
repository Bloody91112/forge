package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"forge/internal/auth"
	"forge/internal/httpresponse"
	"forge/internal/token"
	"forge/internal/worker"

	"github.com/google/uuid"
)

type workerAuthRepositoryStub struct {
	err   error
	calls int
	hash  string
}

func (r *workerAuthRepositoryStub) GetWorkerByTokenHash(
	_ context.Context,
	hash string,
) (worker.Worker, error) {
	r.calls++
	r.hash = hash

	if r.err != nil {
		return worker.Worker{}, r.err
	}

	return worker.Worker{ID: uuid.New()}, nil
}

func TestWorkerAuth(t *testing.T) {
	tests := []struct {
		name          string
		authHeader    string
		repositoryErr error
		wantStatus    int
		wantErrorCode string
		wantNext      bool
		wantRepoCalls int
	}{
		{
			name:          "valid token",
			authHeader:    "Bearer worker-secret",
			wantStatus:    http.StatusNoContent,
			wantNext:      true,
			wantRepoCalls: 1,
		},
		{
			name:          "missing authorization",
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: httpresponse.ErrCodeNotAuthorized,
			wantRepoCalls: 0,
		},
		{
			name:          "invalid authorization",
			authHeader:    "Basic worker-secret",
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: httpresponse.ErrCodeNotAuthorized,
			wantRepoCalls: 0,
		},
		{
			name:          "worker not found",
			authHeader:    "Bearer worker-secret",
			repositoryErr: auth.ErrWorkerNotFound,
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: httpresponse.ErrCodeNotAuthorized,
			wantRepoCalls: 1,
		},
		{
			name:          "unexpected repository error",
			authHeader:    "Bearer worker-secret",
			repositoryErr: errors.New("database unavailable"),
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: httpresponse.ErrCodeAuthFailed,
			wantRepoCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &workerAuthRepositoryStub{err: tt.repositoryErr}
			authService := auth.NewWorkerService(repo)

			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusNoContent)
			})

			handler := WorkerAuth(authService, next)

			req := httptest.NewRequest(http.MethodPost, "/internal/v1/workers/heartbeat", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf(
					"HTTP status = %d, want %d; body: %s",
					recorder.Code,
					tt.wantStatus,
					recorder.Body.String(),
				)
			}

			if nextCalled != tt.wantNext {
				t.Fatalf("next called = %v, want %v", nextCalled, tt.wantNext)
			}

			if repo.calls != tt.wantRepoCalls {
				t.Fatalf("repository calls = %d, want %d", repo.calls, tt.wantRepoCalls)
			}

			if tt.wantErrorCode != "" {
				if got := recorder.Header().Get("Content-Type"); got != "application/json" {
					t.Fatalf("Content-Type = %q, want application/json", got)
				}

				var response httpresponse.ErrorResponse
				if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
					t.Fatalf("decode error response: %v", err)
				}

				if response.Error.Code != tt.wantErrorCode {
					t.Fatalf(
						"error code = %q, want %q",
						response.Error.Code,
						tt.wantErrorCode,
					)
				}
			}

			if tt.wantRepoCalls > 0 {
				if got, want := repo.hash, token.HashToken("worker-secret"); got != want {
					t.Fatalf("repository hash = %q, want %q", got, want)
				}
			}
		})
	}
}
