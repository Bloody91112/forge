package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"forge/internal/token"
	"forge/internal/worker"

	"github.com/google/uuid"
)

type workerTokenRepositoryStub struct {
	worker worker.Worker
	err    error
	hash   string
	calls  int
}

func (r *workerTokenRepositoryStub) GetWorkerByTokenHash(
	_ context.Context,
	hash string,
) (worker.Worker, error) {
	r.calls++
	r.hash = hash
	return r.worker, r.err
}

func TestWorkerService_VerifyWorkerByToken(t *testing.T) {
	repositoryErr := errors.New("database unavailable")

	tests := []struct {
		name          string
		authHeader    string
		repositoryErr error
		wantErr       error
		wantCalls     int
	}{
		{
			name:       "valid token",
			authHeader: "Bearer worker-secret",
			wantCalls:  1,
		},
		{
			name:      "missing authorization header",
			wantErr:   token.ErrInvalidAuthToken,
			wantCalls: 0,
		},
		{
			name:       "invalid authorization scheme",
			authHeader: "Basic worker-secret",
			wantErr:    token.ErrInvalidAuthToken,
			wantCalls:  0,
		},
		{
			name:       "missing bearer token",
			authHeader: "Bearer",
			wantErr:    token.ErrInvalidAuthToken,
			wantCalls:  0,
		},
		{
			name:          "worker not found",
			authHeader:    "Bearer worker-secret",
			repositoryErr: ErrWorkerNotFound,
			wantErr:       ErrWorkerNotFound,
			wantCalls:     1,
		},
		{
			name:          "repository error",
			authHeader:    "Bearer worker-secret",
			repositoryErr: repositoryErr,
			wantErr:       repositoryErr,
			wantCalls:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &workerTokenRepositoryStub{
				worker: worker.Worker{ID: uuid.New()},
				err:    tt.repositoryErr,
			}
			service := NewWorkerService(repo)

			req := httptest.NewRequest(http.MethodPost, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			workerID, err := service.VerifyWorkerByToken(req)

			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("VerifyWorkerByToken() error = %v, want nil", err)
				}

				if workerID == uuid.Nil {
					t.Fatal("got nil worker uuid")
				}

			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf(
					"VerifyWorkerByToken() error = %v, want errors.Is(_, %v)",
					err,
					tt.wantErr,
				)
			}

			if repo.calls != tt.wantCalls {
				t.Fatalf("repository calls = %d, want %d", repo.calls, tt.wantCalls)
			}

			if tt.wantCalls > 0 {
				if got, want := repo.hash, token.HashToken("worker-secret"); got != want {
					t.Fatalf("repository hash = %q, want %q", got, want)
				}

				if repo.hash == "worker-secret" {
					t.Fatal("repository received the plaintext token")
				}
			}
		})
	}
}
