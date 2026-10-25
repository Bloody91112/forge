package token

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestService_ExtractBearerToken(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantToken string
		wantErr   bool
	}{
		{
			name:      "valid bearer token",
			header:    "Bearer abc123",
			wantToken: "abc123",
		},
		{
			name:      "case insensitive scheme",
			header:    "bearer abc123",
			wantToken: "abc123",
		},
		{
			name:      "extra whitespace",
			header:    "  Bearer   abc123  ",
			wantToken: "abc123",
		},
		{
			name:    "missing token",
			header:  "Bearer",
			wantErr: true,
		},
		{
			name:    "whitespace without token",
			header:  "Bearer    ",
			wantErr: true,
		},
		{
			name:    "unsupported scheme",
			header:  "Basic abc123",
			wantErr: true,
		},
		{
			name:    "extra token part",
			header:  "Bearer abc123 extra",
			wantErr: true,
		},
		{
			name:    "empty header",
			header:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodGet,
				"/",
				nil,
			)
			req.Header.Set("Authorization", tt.header)

			gotToken, err := ExtractBearerToken(req)

			if (err != nil) != tt.wantErr {
				t.Fatalf("extractBearerToken() error = %v, wantErr %v", err, tt.wantErr)
			}

			if gotToken != tt.wantToken {
				t.Errorf("extractBearerToken() token = %q, want %q", gotToken, tt.wantToken)
			}
		})
	}
}

func TestService_MakeTokenHash(t *testing.T) {
	token := uuid.New().String()
	hash := HashToken(token)

	if hash == token {
		t.Fatalf("token hash equals token: %s", token)
	}

	hashSecondCall := HashToken(token)

	if hash != hashSecondCall {
		t.Fatalf("token hash changed after func second call, initial: %s, second: %s", hash, hashSecondCall)
	}
}
