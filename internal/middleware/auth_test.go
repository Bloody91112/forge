package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"forge/internal/auth"
	"forge/internal/httpresponse"
	"forge/internal/task"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

type ClientRepositoryStub struct {
	client auth.Client
	err    error
}

func (r *ClientRepositoryStub) GetClientByTokenHash(ctx context.Context, hash string) (auth.Client, error) {
	return r.client, r.err
}

func TestAuth(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		authHeader   string
		wantID       uuid.UUID
		wantHttpCode int
		wantErrCode  string
		repoErr      error
	}{
		{
			name:         "happy path",
			authHeader:   "Bearer 123",
			path:         "/any",
			wantID:       uuid.New(),
			wantHttpCode: http.StatusOK,
		},
		{
			name:         "no auth header",
			path:         "/any",
			wantHttpCode: http.StatusUnauthorized,
			wantErrCode:  httpresponse.ErrCodeNotAuthorized,
		},
		{
			name:         "bad auth header",
			authHeader:   "baaad",
			path:         "/any",
			wantHttpCode: http.StatusUnauthorized,
			wantErrCode:  httpresponse.ErrCodeNotAuthorized,
		},
		{
			name:         "client not found",
			authHeader:   "Bearer 123",
			path:         "/any",
			repoErr:      auth.ErrClientNotFound,
			wantHttpCode: http.StatusUnauthorized,
			wantErrCode:  httpresponse.ErrCodeNotAuthorized,
		},
		{
			name:         "unknown repo error",
			authHeader:   "Bearer 123",
			path:         "/any",
			repoErr:      errors.New("something went wrong (test)"),
			wantHttpCode: http.StatusInternalServerError,
			wantErrCode:  httpresponse.ErrCodeAuthFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()

			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			nextCalled := false
			clientIDFromContext := uuid.Nil

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				clientID, ok := task.ClientIDFromContext(r.Context())
				if ok {
					clientIDFromContext = clientID
				}
			})

			client := auth.Client{
				ID:        tt.wantID,
				Name:      "BOB",
				TokenHash: "123",
				CreatedAt: time.Now(),
			}

			repo := ClientRepositoryStub{client: client, err: tt.repoErr}
			authService := auth.NewClientService(&repo)
			middleware := Auth(authService, handler)

			middleware.ServeHTTP(rr, req)

			if tt.wantHttpCode == http.StatusOK && !nextCalled {
				t.Fatalf("next http handler was not called")
			}

			if tt.wantErrCode != "" && nextCalled {
				t.Fatalf("next http handler unexpectedly was called")
			}

			if rr.Code != tt.wantHttpCode {
				t.Fatalf("expected http code: %d, got: %d", tt.wantHttpCode, rr.Code)
			}

			if tt.wantErrCode != "" {
				var responseErr httpresponse.ErrorResponse
				if err := json.NewDecoder(rr.Body).Decode(&responseErr); err != nil {
					t.Fatalf("failed to decode error response: %v", err)
				}

				if responseErr.Error.Code != tt.wantErrCode {
					t.Errorf("expected error code: %s, got: %s", tt.wantErrCode, responseErr.Error.Code)
				}
			}

			if tt.wantID != uuid.Nil {
				if tt.wantID != clientIDFromContext {
					t.Fatalf("expected client id: %s, got: %s", tt.wantID, clientIDFromContext)
				}
			}
		})
	}
}
