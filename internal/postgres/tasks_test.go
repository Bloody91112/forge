package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"forge/internal/task"
	"forge/internal/testutil"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
)

func TestTaskRepository_CreateOrGetByIdempotencyKey(t *testing.T) {
	db, err := testutil.OpenPostgres(t, true)

	if err != nil {
		t.Fatalf("test db connection failed: %v", err)
	}

	clientId, err := testutil.CreateTestClient(t, db)

	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	repo := NewTaskRepository(db)

	idempotencyKey := uuid.New().String()
	var maxAttempts int64 = 12
	var timeout int64 = 100

	expected := task.Task{
		ID:             uuid.New(),
		ClientID:       clientId,
		IdempotencyKey: idempotencyKey,
		RequestHash:    "456",
		Type:           "email-send",
		Payload:        []byte(`{"user_id":42}`),
		Status:         task.StatusQueued,
		MaxAttempts:    maxAttempts,
		TimeoutSeconds: timeout,
	}

	actual, created, err := repo.CreateOrGetByIdempotencyKey(t.Context(), expected)

	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	if !created {
		t.Fatalf("created param expected to be true, got false")
	}

	if diff := cmp.Diff(expected, actual, cmpopts.IgnoreFields(task.Task{}, "CreatedAt", "AvailableAt", "Payload")); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}

	assertJSONEqual(t, expected.Payload, actual.Payload)

	if actual.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}

	if actual.AvailableAt.IsZero() {
		t.Error("AvailableAt should not be zero")
	}

	repeatResult, repeatCreated, repeatErr := repo.CreateOrGetByIdempotencyKey(t.Context(), expected)
	if repeatErr != nil {
		t.Fatalf("failed repeat request: %v", repeatErr)
	}

	if repeatCreated {
		t.Fatalf("repeat create param expected to be false, got true")
	}

	if diff := cmp.Diff(expected, repeatResult, cmpopts.IgnoreFields(task.Task{}, "CreatedAt", "AvailableAt", "Payload")); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}

	otherClientId, err := testutil.CreateTestClient(t, db)

	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	otherClientTaskWithSameKey := task.Task{
		ID:             uuid.New(),
		ClientID:       otherClientId,
		IdempotencyKey: idempotencyKey,
		RequestHash:    "456",
		Type:           "email-send",
		Payload:        []byte(`{"user_id":40}`),
		Status:         task.StatusQueued,
		MaxAttempts:    maxAttempts,
		TimeoutSeconds: timeout,
	}

	otherResult, otherCreated, otherErr := repo.CreateOrGetByIdempotencyKey(t.Context(), otherClientTaskWithSameKey)

	if otherErr != nil {
		t.Fatalf("failed other client request: %v", otherErr)
	}

	if !otherCreated {
		t.Fatalf("other create param expected to be true, got false")
	}

	if diff := cmp.Diff(otherClientTaskWithSameKey, otherResult, cmpopts.IgnoreFields(task.Task{}, "CreatedAt", "AvailableAt", "Payload")); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}

	assertJSONEqual(t, otherClientTaskWithSameKey.Payload, otherResult.Payload)

}

func TestTaskRepository_GetByID(t *testing.T) {
	db, err := testutil.OpenPostgres(t, true)

	if err != nil {
		t.Fatalf("test db connection failed: %v", err)
	}

	clientId, err := testutil.CreateTestClient(t, db)

	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	repo := NewTaskRepository(db)

	expected := task.Task{
		ID:             uuid.New(),
		ClientID:       clientId,
		IdempotencyKey: uuid.New().String(),
		RequestHash:    "456",
		Type:           "email-send",
		Payload:        []byte(`{"user_id":42}`),
		Status:         task.StatusQueued,
		MaxAttempts:    12,
		TimeoutSeconds: 100,
	}

	_, _, err = repo.CreateOrGetByIdempotencyKey(t.Context(), expected)

	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	got, err := repo.GetByID(t.Context(), expected.ID, clientId)

	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}

	if diff := cmp.Diff(expected, got, cmpopts.IgnoreFields(task.Task{}, "CreatedAt", "AvailableAt", "Payload")); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}

	assertJSONEqual(t, expected.Payload, got.Payload)

	unknownTaskID := uuid.New()

	_, err = repo.GetByID(t.Context(), unknownTaskID, clientId)

	if !errors.Is(err, task.ErrTaskNotFound) {
		t.Fatalf("unexpected err, expected err: %v, got: %v", task.ErrTaskNotFound, err)
	}

}

func TestTaskRepository_CreateOrGetByIdempotencyKey_WithConcurrency(t *testing.T) {
	db, err := testutil.OpenPostgres(t, true)

	if err != nil {
		t.Fatalf("test db connection failed: %v", err)
	}

	clientId, err := testutil.CreateTestClient(t, db)

	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	repo := NewTaskRepository(db)

	taskID := uuid.New()
	idempotencyKey := uuid.New().String()

	taskToCreate := task.Task{
		ID:             taskID,
		ClientID:       clientId,
		IdempotencyKey: idempotencyKey,
		RequestHash:    "456",
		Type:           "email-send",
		Payload:        []byte(`{"user_id":42}`),
		Status:         task.StatusQueued,
		MaxAttempts:    12,
		TimeoutSeconds: 100,
	}

	var wg sync.WaitGroup
	gCount := 100

	wg.Add(gCount)

	for range gCount {
		go func() {
			defer wg.Done()
			got, _, err := repo.CreateOrGetByIdempotencyKey(t.Context(), taskToCreate)
			if err != nil {
				t.Errorf("CreateOrGetByIdempotencyKey failed: %v", err)
				return
			}

			if got.ID != taskID {
				t.Errorf("unexpected task, expected:%s got:%s", taskID.String(), got.ID.String())
			}
		}()
	}

	wg.Wait()

	var countRows int

	countRowsErr := db.QueryRow(t.Context(), "SELECT COUNT(*) FROM tasks WHERE idempotency_key = $1 AND client_id = $2", idempotencyKey, clientId).Scan(&countRows)

	if countRowsErr != nil {
		t.Fatalf("count result rows failed: %v", countRowsErr)
	}

	if countRows != 1 {
		t.Fatalf("expected row count, expected: %d, got:%d", 1, countRows)
	}
}

func TestTaskRepository_GetByID_ReturnsErrWhenTaskBelongsToAnotherClient(t *testing.T) {
	db, err := testutil.OpenPostgres(t, true)

	if err != nil {
		t.Fatalf("test db connection failed: %v", err)
	}

	clientId, err := testutil.CreateTestClient(t, db)

	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	repo := NewTaskRepository(db)

	expected := task.Task{
		ID:             uuid.New(),
		ClientID:       clientId,
		IdempotencyKey: uuid.New().String(),
		RequestHash:    "456",
		Type:           "email-send",
		Payload:        []byte(`{"user_id":42}`),
		Status:         task.StatusQueued,
		MaxAttempts:    12,
		TimeoutSeconds: 100,
	}

	otherClientId, err := testutil.CreateTestClient(t, db)

	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, _, err = repo.CreateOrGetByIdempotencyKey(t.Context(), expected)

	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	_, err = repo.GetByID(t.Context(), expected.ID, otherClientId)

	if !errors.Is(err, task.ErrTaskNotFound) {
		t.Fatalf("expected err: %v, got: %v", task.ErrTaskNotFound, err)
	}

}

func assertJSONEqual(t *testing.T, expected, actual json.RawMessage) {
	t.Helper()

	var want, got any

	decode := func(data json.RawMessage, dst *any) error {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		return decoder.Decode(dst)
	}

	if err := decode(expected, &want); err != nil {
		t.Fatalf("decode expected JSON: %v", err)
	}

	if err := decode(actual, &got); err != nil {
		t.Fatalf("decode actual JSON: %v", err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("JSON mismatch (-want +got):\n%s", diff)
	}
}
