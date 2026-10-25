package auth

import (
	"context"
	"errors"
	"forge/internal/token"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

type ClientRepositoryStub struct {
	client Client
	err    error
}

func (r *ClientRepositoryStub) GetClientByTokenHash(ctx context.Context, hash string) (Client, error) {
	return r.client, r.err
}

func TestService_VerifyClientByToken(t *testing.T) {

	clientID := uuid.New()
	clientToken := uuid.New().String()

	tests := []struct {
		name       string
		authHeader string
		wantID     uuid.UUID
		wantErr    error
	}{
		{
			name:       "correct auth header",
			authHeader: "Bearer " + clientToken,
			wantID:     clientID,
			wantErr:    nil,
		},
		{
			name:       "bad auth header",
			authHeader: "Bearer",
			wantID:     uuid.Nil,
			wantErr:    token.ErrInvalidAuthToken,
		},
		{
			name:       "client not found",
			authHeader: "Bearer " + clientToken,
			wantID:     uuid.Nil,
			wantErr:    ErrClientNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", tt.authHeader)

			client := Client{
				ID:        tt.wantID,
				Name:      "BOB",
				TokenHash: "123",
				CreatedAt: time.Now(),
			}

			repo := ClientRepositoryStub{client, tt.wantErr}
			service := NewService(&repo)
			id, serviceErr := service.VerifyClientByToken(req)

			if !errors.Is(serviceErr, tt.wantErr) {
				t.Fatalf("expected err: %v, got: %v", tt.wantErr, serviceErr)
			}

			if tt.wantID != id {
				t.Fatalf("expected id: %s, got: %s", tt.wantID, id)
			}
		})
	}
}
