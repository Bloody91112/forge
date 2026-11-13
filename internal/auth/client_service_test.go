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

type clientRepositoryStub struct {
	client Client
	err    error
	hash   string
	calls  int

	createdClient Client
	createErr     error
	createCalls   int

	rotatedClient Client
	rotateErr     error
	rotatedID     uuid.UUID
	rotatedHash   string
	rotateCalls   int
}

func (r *clientRepositoryStub) GetClientByTokenHash(
	_ context.Context,
	hash string,
) (Client, error) {
	r.calls++
	r.hash = hash

	return r.client, r.err
}

func (r *clientRepositoryStub) CreateClient(_ context.Context, client Client) (Client, error) {
	r.createCalls++
	r.createdClient = client

	if r.createErr != nil {
		return Client{}, r.createErr
	}

	return client, nil
}

func (r *clientRepositoryStub) RotateClientToken(_ context.Context, id uuid.UUID, hash string) (Client, error) {
	r.rotateCalls++
	r.rotatedID = id
	r.rotatedHash = hash

	if r.rotateErr != nil {
		return Client{}, r.rotateErr
	}

	return r.rotatedClient, nil
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

func TestClientService_CreateClient(t *testing.T) {
	repo := &clientRepositoryStub{}
	service := NewClientService(repo)

	client, clientToken, err := service.CreateClient(t.Context(), "test-client")
	if err != nil {
		t.Fatalf("CreateClient() error = %v", err)
	}

	if client.ID == uuid.Nil {
		t.Error("client ID is empty")
	}

	if client.Name != "test-client" {
		t.Errorf("client name = %q, want %q", client.Name, "test-client")
	}

	if len(clientToken) != 64 {
		t.Errorf("client token length = %d, want 64", len(clientToken))
	}

	if repo.createCalls != 1 {
		t.Fatalf("CreateClient repository calls = %d, want 1", repo.createCalls)
	}

	if repo.createdClient.TokenHash != token.HashToken(clientToken) {
		t.Error("repository did not receive the client token hash")
	}

	if repo.createdClient.TokenHash == clientToken {
		t.Error("repository received the plaintext token")
	}

	if client.TokenHash != repo.createdClient.TokenHash {
		t.Error("returned client token hash differs from stored client hash")
	}
}

func TestClientService_CreateClientDoesNotReturnTokenOnRepositoryError(t *testing.T) {
	repositoryErr := errors.New("database unavailable")
	repo := &clientRepositoryStub{createErr: repositoryErr}
	service := NewClientService(repo)

	_, clientToken, err := service.CreateClient(t.Context(), "test-client")

	if !errors.Is(err, repositoryErr) {
		t.Fatalf("CreateClient() error = %v, want %v", err, repositoryErr)
	}

	if clientToken != "" {
		t.Error("plaintext token was returned after repository failure")
	}
}

func TestClientService_RotateClientToken(t *testing.T) {
	clientID := uuid.New()
	previousHash := "previous-token-hash"
	expiresAt := time.Now()

	repo := &clientRepositoryStub{
		rotatedClient: Client{
			ID:                     clientID,
			PreviousTokenHash:      &previousHash,
			PreviousTokenExpiresAt: &expiresAt,
		},
	}
	service := NewClientService(repo)

	client, clientToken, err := service.RotateClientToken(t.Context(), clientID)
	if err != nil {
		t.Fatalf("RotateClientToken() error = %v", err)
	}

	if repo.rotateCalls != 1 {
		t.Fatalf("RotateClientToken repository calls = %d, want 1", repo.rotateCalls)
	}

	if repo.rotatedID != clientID {
		t.Errorf("rotated client ID = %s, want %s", repo.rotatedID, clientID)
	}

	if len(clientToken) != 64 {
		t.Errorf("client token length = %d, want 64", len(clientToken))
	}

	if repo.rotatedHash != token.HashToken(clientToken) {
		t.Error("repository did not receive the new client token hash")
	}

	if client.ID != clientID {
		t.Errorf("client ID = %s, want %s", client.ID, clientID)
	}

	if client.PreviousTokenHash == nil || *client.PreviousTokenHash != previousHash {
		t.Error("previous token hash was not returned")
	}

	if client.PreviousTokenExpiresAt == nil {
		t.Error("previous token expiration was not returned")
	}
}

func TestClientService_RotateClientTokenDoesNotReturnTokenOnRepositoryError(t *testing.T) {
	clientID := uuid.New()
	repositoryErr := errors.New("database unavailable")
	repo := &clientRepositoryStub{rotateErr: repositoryErr}
	service := NewClientService(repo)

	_, clientToken, err := service.RotateClientToken(t.Context(), clientID)

	if !errors.Is(err, repositoryErr) {
		t.Fatalf("RotateClientToken() error = %v, want %v", err, repositoryErr)
	}

	if clientToken != "" {
		t.Error("plaintext token was returned after repository failure")
	}
}
