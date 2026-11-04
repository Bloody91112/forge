package forgeapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := NewClient(server.Client(), server.URL+"/")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return client
}

func assertRequest(t *testing.T, req *http.Request, wantPath string, wantToken string) {
	t.Helper()

	if req.Method != http.MethodPost {
		t.Errorf("method = %q, want %q", req.Method, http.MethodPost)
	}

	if req.URL.Path != wantPath {
		t.Errorf("path = %q, want %q", req.URL.Path, wantPath)
	}

	if got := req.Header.Get("Authorization"); got != "Bearer "+wantToken {
		t.Errorf("Authorization = %q, want %q", got, "Bearer "+wantToken)
	}

	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

func decodeRequest(t *testing.T, req *http.Request, output any) {
	t.Helper()

	if err := json.NewDecoder(req.Body).Decode(output); err != nil {
		t.Errorf("decode request body: %v", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, output any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(output); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func TestNewClient_Validation(t *testing.T) {
	if _, err := NewClient(nil, "http://localhost"); err == nil {
		t.Fatal("expected error for nil HTTP client")
	}

	if _, err := NewClient(http.DefaultClient, ""); err == nil {
		t.Fatal("expected error for empty base URL")
	}
}

func TestClient_Register(t *testing.T) {
	workerID := uuid.New()
	expiresAt := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

	input := RegisterRequest{
		WorkerID:        workerID,
		ProtocolVersion: "1",
		Capabilities:    []string{"email.send"},
	}

	wantResponse := RegisterResponse{
		WorkerID:       workerID,
		WorkerToken:    "worker-token",
		LeaseExpiresAt: expiresAt,
	}

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, "/internal/v1/workers/register", "bootstrap-token")

		var got RegisterRequest
		decodeRequest(t, r, &got)

		if !reflect.DeepEqual(got, input) {
			t.Errorf("request body = %+v, want %+v", got, input)
		}

		writeJSON(t, w, wantResponse)
	}))

	got, err := client.Register(
		t.Context(),
		"bootstrap-token",
		input,
	)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if got.WorkerID != wantResponse.WorkerID {
		t.Errorf("WorkerID = %s, want %s", got.WorkerID, wantResponse.WorkerID)
	}

	if got.WorkerToken != wantResponse.WorkerToken {
		t.Errorf("WorkerToken = %q, want %q", got.WorkerToken, wantResponse.WorkerToken)
	}

	if !got.LeaseExpiresAt.Equal(wantResponse.LeaseExpiresAt) {
		t.Errorf("LeaseExpiresAt = %v, want %v", got.LeaseExpiresAt, wantResponse.LeaseExpiresAt)
	}
}

func TestClient_Heartbeat(t *testing.T) {
	workerID := uuid.New()
	expiresAt := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

	input := HeartbeatRequest{
		WorkerID: workerID,
	}

	wantResponse := HeartbeatResponse{
		WorkerID:       workerID,
		LeaseExpiresAt: expiresAt,
	}

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, "/internal/v1/workers/heartbeat", "worker-token")

		var got HeartbeatRequest
		decodeRequest(t, r, &got)

		if !reflect.DeepEqual(got, input) {
			t.Errorf("request body = %+v, want %+v", got, input)
		}

		writeJSON(t, w, wantResponse)
	}))

	got, err := client.Heartbeat(t.Context(), "worker-token", input)
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}

	if got.WorkerID != wantResponse.WorkerID {
		t.Errorf("WorkerID = %s, want %s", got.WorkerID, wantResponse.WorkerID)
	}

	if !got.LeaseExpiresAt.Equal(wantResponse.LeaseExpiresAt) {
		t.Errorf("LeaseExpiresAt = %v, want %v", got.LeaseExpiresAt, wantResponse.LeaseExpiresAt)
	}
}

func TestClient_ClaimTask(t *testing.T) {
	workerID := uuid.New()
	taskID := uuid.New()
	expiresAt := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

	input := ClaimTaskRequest{
		WorkerID: workerID,
	}

	t.Run("task available", func(t *testing.T) {
		wantResponse := ClaimTaskResponse{
			Task: ClaimedTask{
				ID:             taskID,
				Type:           "email.send",
				Payload:        json.RawMessage(`{"recipient":"test@example.com"}`),
				AttemptNumber:  2,
				TimeoutSeconds: 300,
				LeaseToken:     "lease-token",
				LeaseExpiresAt: expiresAt,
			},
		}

		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertRequest(t, r, "/internal/v1/tasks/claim", "worker-token")

			var got ClaimTaskRequest
			decodeRequest(t, r, &got)

			if !reflect.DeepEqual(got, input) {
				t.Errorf("request body = %+v, want %+v", got, input)
			}

			writeJSON(t, w, wantResponse)
		}))

		got, err := client.ClaimTask(t.Context(), "worker-token", input)
		if err != nil {
			t.Fatalf("ClaimTask() error = %v", err)
		}

		if got == nil {
			t.Fatal("ClaimTask() returned nil response")
		}

		if got.Task.ID != taskID {
			t.Errorf("Task.ID = %s, want %s", got.Task.ID, taskID)
		}

		if got.Task.Type != wantResponse.Task.Type {
			t.Errorf("Task.Type = %q, want %q", got.Task.Type, wantResponse.Task.Type)
		}

		if !reflect.DeepEqual(got.Task.Payload, wantResponse.Task.Payload) {
			t.Errorf("Task.Payload = %s, want %s", got.Task.Payload, wantResponse.Task.Payload)
		}

		if got.Task.AttemptNumber != wantResponse.Task.AttemptNumber {
			t.Errorf("Task.AttemptNumber = %d, want %d", got.Task.AttemptNumber, wantResponse.Task.AttemptNumber)
		}

		if got.Task.TimeoutSeconds != wantResponse.Task.TimeoutSeconds {
			t.Errorf("Task.TimeoutSeconds = %d, want %d", got.Task.TimeoutSeconds, wantResponse.Task.TimeoutSeconds)
		}

		if got.Task.LeaseToken != wantResponse.Task.LeaseToken {
			t.Errorf("Task.LeaseToken = %q, want %q", got.Task.LeaseToken, wantResponse.Task.LeaseToken)
		}

		if !got.Task.LeaseExpiresAt.Equal(expiresAt) {
			t.Errorf("Task.LeaseExpiresAt = %v, want %v", got.Task.LeaseExpiresAt, expiresAt)
		}
	})

	t.Run("no task available", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertRequest(t, r, "/internal/v1/tasks/claim", "worker-token")

			var got ClaimTaskRequest
			decodeRequest(t, r, &got)

			if !reflect.DeepEqual(got, input) {
				t.Errorf("request body = %+v, want %+v", got, input)
			}

			w.WriteHeader(http.StatusNoContent)
		}))

		got, err := client.ClaimTask(t.Context(), "worker-token", input)
		if err != nil {
			t.Fatalf("ClaimTask() error = %v", err)
		}

		if got != nil {
			t.Errorf("ClaimTask() response = %+v, want nil", got)
		}
	})
}

func TestClient_TaskLifecycle(t *testing.T) {
	workerID := uuid.New()
	taskID := uuid.New()
	expiresAt := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		path     string
		wantBody map[string]any
		response any
		call     func(*Client) error
	}{
		{
			name: "renew",
			path: "/internal/v1/tasks/" + taskID.String() + "/renew",
			wantBody: map[string]any{
				"worker_id":   workerID.String(),
				"lease_token": "lease-token",
			},
			response: RenewTaskResponse{
				LeaseExpiresAt: expiresAt,
			},
			call: func(client *Client) error {
				got, err := client.RenewTaskLease(
					t.Context(),
					"worker-token",
					taskID,
					RenewTaskRequest{
						WorkerID:   workerID,
						LeaseToken: "lease-token",
					},
				)
				if err != nil {
					return err
				}

				if !got.LeaseExpiresAt.Equal(expiresAt) {
					return errors.New("unexpected lease expiration")
				}

				return nil
			},
		},
		{
			name: "complete",
			path: "/internal/v1/tasks/" + taskID.String() + "/complete",
			wantBody: map[string]any{
				"worker_id":   workerID.String(),
				"lease_token": "lease-token",
				"result": map[string]any{
					"sent": true,
				},
			},
			call: func(client *Client) error {
				return client.CompleteTask(
					t.Context(),
					"worker-token",
					taskID,
					CompleteTaskRequest{
						WorkerID:   workerID,
						LeaseToken: "lease-token",
						Result:     json.RawMessage(`{"sent":true}`),
					},
				)
			},
		},
		{
			name: "fail",
			path: "/internal/v1/tasks/" + taskID.String() + "/fail",
			wantBody: map[string]any{
				"worker_id":   workerID.String(),
				"lease_token": "lease-token",
				"error": map[string]any{
					"code":    "delivery_failed",
					"message": "SMTP connection failed",
				},
				"retryable": true,
			},
			call: func(client *Client) error {
				return client.FailTask(
					t.Context(),
					"worker-token",
					taskID,
					FailTaskRequest{
						WorkerID:   workerID,
						LeaseToken: "lease-token",
						Error: TaskError{
							Code:    "delivery_failed",
							Message: "SMTP connection failed",
						},
						Retryable: true,
					},
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assertRequest(t, r, tt.path, "worker-token")

				var got map[string]any
				decodeRequest(t, r, &got)

				if !reflect.DeepEqual(got, tt.wantBody) {
					t.Errorf("request body = %+v, want %+v", got, tt.wantBody)
				}

				if tt.response != nil {
					writeJSON(t, w, tt.response)
					return
				}

				w.WriteHeader(http.StatusOK)
			}))

			if err := tt.call(client); err != nil {
				t.Fatalf("%s() error = %v", tt.name, err)
			}
		})
	}
}

func TestClient_RemoteError(t *testing.T) {
	taskID := uuid.New()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)

		_, _ = w.Write([]byte(`{
			"error": {
				"code": "lease_lost",
				"message": "Task lease is no longer valid"
			}
		}`))
	}))

	_, err := client.RenewTaskLease(
		t.Context(),
		"worker-token",
		taskID,
		RenewTaskRequest{
			WorkerID:   uuid.New(),
			LeaseToken: "lease-token",
		},
	)
	if err == nil {
		t.Fatal("expected remote error")
	}

	var remoteErr *RemoteError
	if !errors.As(err, &remoteErr) {
		t.Fatalf("error type = %T, want *RemoteError", err)
	}

	if remoteErr.StatusCode != http.StatusConflict {
		t.Errorf("StatusCode = %d, want %d", remoteErr.StatusCode, http.StatusConflict)
	}

	if remoteErr.Code != "lease_lost" {
		t.Errorf("Code = %q, want lease_lost", remoteErr.Code)
	}

	if remoteErr.Message != "Task lease is no longer valid" {
		t.Errorf("Message = %q, want %q", remoteErr.Message, "Task lease is no longer valid")
	}
}

func TestClient_TransportError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	_, err = client.Heartbeat(
		t.Context(),
		"worker-token",
		HeartbeatRequest{WorkerID: uuid.New()},
	)
	if err == nil {
		t.Fatal("expected transport error")
	}

	var remoteErr *RemoteError
	if errors.As(err, &remoteErr) {
		t.Fatalf("expected transport error, got remote error: %v", err)
	}
}
