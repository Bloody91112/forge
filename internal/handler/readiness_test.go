package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type DBPinglerStub struct {
	Result error
}

func (db *DBPinglerStub) Ping(ctx context.Context) error {
	return db.Result
}

func TestReadinessHandler_CheckWhenReady(t *testing.T) {

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()

	pingler := DBPinglerStub{}
	handler := NewReadinessHandler(&pingler)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /readyz", handler.Check)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestReadinessHandler_CheckWhenNotReady(t *testing.T) {

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()

	pingler := DBPinglerStub{errors.New("db down")}
	handler := NewReadinessHandler(&pingler)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /readyz", handler.Check)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}

func TestReadinessHandler_SetNotReady(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()

	pingler := DBPinglerStub{}
	handler := NewReadinessHandler(&pingler)
	handler.SetNotReady()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /readyz", handler.Check)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}
