package auth

import (
	"context"
	"errors"
	"forge/internal/token"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

type clientRepositoryStub struct {
	client Client
	err    error
	hash   string
	calls  int
}

func (r *clientRepositoryStub) GetClientByTokenHash(
	_ context.Context,
	hash string,
) (Client, error) {
	r.calls++
	r.hash = hash

	return r.client, r.err
}

func TestClientService_VerifyClientByToken(t *testing.T) {
	clientToken := "client-secret"
	clientID := uuid.New()
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
			authHeader: "Bearer " + clientToken,
			wantID:     clientID,
			wantCalls:  1,
		},
		{
			name:      "missing authorization header",
			wantErr:   token.ErrInvalidAuthToken,
			wantCalls: 0,
		},
		{
			name:       "invalid authorization scheme",
			authHeader: "Basic " + clientToken,
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
			name:          "client not found",
			authHeader:    "Bearer " + clientToken,
			repositoryErr: ErrClientNotFound,
			wantErr:       ErrClientNotFound,
			wantCalls:     1,
		},
		{
			name:          "repository error",
			authHeader:    "Bearer " + clientToken,
			repositoryErr: repositoryErr,
			wantErr:       repositoryErr,
			wantCalls:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &clientRepositoryStub{
				client: Client{ID: clientID},
				err:    tt.repositoryErr,
			}
			service := NewClientService(repo)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			gotID, err := service.VerifyClientByToken(req)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf(
						"VerifyClientByToken() error = %v, want errors.Is(_, %v)",
						err,
						tt.wantErr,
					)
				}
			} else if err != nil {
				t.Fatalf("VerifyClientByToken() error = %v, want nil", err)
			}

			if gotID != tt.wantID {
				t.Errorf("VerifyClientByToken() ID = %s, want %s", gotID, tt.wantID)
			}

			if repo.calls != tt.wantCalls {
				t.Fatalf("repository calls = %d, want %d", repo.calls, tt.wantCalls)
			}

			if tt.wantCalls > 0 {
				wantHash := token.HashToken(clientToken)
				if repo.hash != wantHash {
					t.Errorf("repository hash = %q, want %q", repo.hash, wantHash)
				}
				if repo.hash == clientToken {
					t.Error("repository received the plaintext token")
				}
			}
		})
	}
}
