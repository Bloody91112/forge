package middleware

import (
	"context"
	"encoding/json"
	"forge/internal/auth"
	"forge/internal/httpresponse"
	"forge/internal/task"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

type ClientRepositoryStub struct {
	clientID uuid.UUID
	err      error
}

func (r *ClientRepositoryStub) GetClientIDByTokenHash(ctx context.Context, hash string) (uuid.UUID, error) {
	return r.clientID, r.err
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
			name:         "no auth for healthz route",
			path:         "/healthz",
			wantHttpCode: http.StatusOK,
		},
		{
			name:         "no auth for readyz route",
			path:         "/readyz",
			wantHttpCode: http.StatusOK,
		},
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

			repo := ClientRepositoryStub{clientID: tt.wantID, err: tt.repoErr}
			authService := auth.NewService(&repo)
			middleware := Auth(authService, handler)

			middleware.ServeHTTP(rr, req)

			if tt.wantHttpCode == http.StatusOK && !nextCalled {
				t.Fatalf("next http handler was not called")
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
