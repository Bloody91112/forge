package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"forge/internal/httpresponse"
)

func newJSONRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func assertJSONContentType(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()

	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

func assertJSONErrorResponse(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
	wantStatus int,
	wantCode string,
) {
	t.Helper()

	if recorder.Code != wantStatus {
		t.Fatalf(
			"HTTP status = %d, want %d; body: %s",
			recorder.Code,
			wantStatus,
			recorder.Body.String(),
		)
	}

	assertJSONContentType(t, recorder)

	var response httpresponse.ErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}

	if response.Error.Code != wantCode {
		t.Fatalf("error code = %q, want %q", response.Error.Code, wantCode)
	}
}
