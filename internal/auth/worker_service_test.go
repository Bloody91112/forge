package auth

import (
	"context"
	"errors"
	"forge/internal/token"
	"forge/internal/worker"
	"net/http"
	"net/http/httptest"
	"testing"

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
	workerToken := "worker-secret"
	workerID := uuid.New()
	repositoryErr := errors.New("database unavailable")

	tests := []struct {
		name          string
		authHeader    string
		repositoryErr error
		wantID        uuid.UUID
		wantErr       error
		wantCalls     int
	}{
		{
			name:       "valid token",
			authHeader: "Bearer " + workerToken,
			wantID:     workerID,
			wantCalls:  1,
		},
		{
			name:      "missing authorization header",
			wantErr:   token.ErrInvalidAuthToken,
			wantCalls: 0,
		},
		{
			name:       "invalid authorization scheme",
			authHeader: "Basic " + workerToken,
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
			authHeader:    "Bearer " + workerToken,
			repositoryErr: ErrWorkerNotFound,
			wantErr:       ErrWorkerNotFound,
			wantCalls:     1,
		},
		{
			name:          "repository error",
			authHeader:    "Bearer " + workerToken,
			repositoryErr: repositoryErr,
			wantErr:       repositoryErr,
			wantCalls:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &workerTokenRepositoryStub{
				worker: worker.Worker{ID: workerID},
				err:    tt.repositoryErr,
			}
			service := NewWorkerService(repo)

			req := httptest.NewRequest(http.MethodPost, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			gotID, err := service.VerifyWorkerByToken(req)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf(
						"VerifyWorkerByToken() error = %v, want errors.Is(_, %v)",
						err,
						tt.wantErr,
					)
				}
			} else if err != nil {
				t.Fatalf("VerifyWorkerByToken() error = %v, want nil", err)
			}

			if gotID != tt.wantID {
				t.Errorf("VerifyWorkerByToken() ID = %s, want %s", gotID, tt.wantID)
			}

			if repo.calls != tt.wantCalls {
				t.Fatalf("repository calls = %d, want %d", repo.calls, tt.wantCalls)
			}

			if tt.wantCalls > 0 {
				wantHash := token.HashToken(workerToken)
				if repo.hash != wantHash {
					t.Errorf("repository hash = %q, want %q", repo.hash, wantHash)
				}
				if repo.hash == workerToken {
					t.Error("repository received the plaintext token")
				}
			}
		})
	}
}
