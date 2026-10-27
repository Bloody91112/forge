package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakePinger struct {
	Result error
}

func (db *fakePinger) Ping(ctx context.Context) error {
	return db.Result
}

func TestReadinessHandler_Check(t *testing.T) {
	tests := []struct {
		name           string
		pingErr        error
		setNotReady    bool
		wantHTTPStatus int
	}{
		{
			name:           "ready",
			wantHTTPStatus: http.StatusOK,
		},
		{
			name:           "database unavailable",
			pingErr:        errors.New("db down"),
			wantHTTPStatus: http.StatusServiceUnavailable,
		},
		{
			name:           "shutdown started",
			setNotReady:    true,
			wantHTTPStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinger := fakePinger{Result: tt.pingErr}
			handler := NewReadinessHandler(&pinger)

			if tt.setNotReady {
				handler.SetNotReady()
			}

			mux := http.NewServeMux()
			mux.HandleFunc("GET /readyz", handler.Check)

			req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantHTTPStatus {
				t.Errorf(
					"HTTP status = %d, want %d",
					recorder.Code,
					tt.wantHTTPStatus,
				)
			}
		})
	}
}
