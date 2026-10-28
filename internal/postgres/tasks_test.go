package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"forge/internal/task"
	"forge/internal/testutil"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTaskRepository_CreateOrGetByIdempotencyKey(t *testing.T) {
	db := openTestDB(t)
	clientID := createTestClient(t, db)
	repo := NewTaskRepository(db)
	expected := newTestTask(clientID)

	got, created, err := repo.CreateOrGetByIdempotencyKey(t.Context(), expected)
	if err != nil {
		t.Fatalf("CreateOrGetByIdempotencyKey() error = %v", err)
	}
	if !created {
		t.Fatal("CreateOrGetByIdempotencyKey() created = false, want true")
	}

	assertTaskEqual(t, expected, got)

	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if got.AvailableAt.IsZero() {
		t.Error("AvailableAt is zero")
	}
}

func TestTaskRepository_CreateOrGetByIdempotencyKey_ReturnsExistingTask(t *testing.T) {
	db := openTestDB(t)
	clientID := createTestClient(t, db)
	repo := NewTaskRepository(db)
	expected := newTestTask(clientID)

	first, created, err := repo.CreateOrGetByIdempotencyKey(t.Context(), expected)
	if err != nil {
		t.Fatalf("first CreateOrGetByIdempotencyKey() error = %v", err)
	}
	if !created {
		t.Fatal("first CreateOrGetByIdempotencyKey() created = false, want true")
	}

	got, created, err := repo.CreateOrGetByIdempotencyKey(t.Context(), expected)
	if err != nil {
		t.Fatalf("repeated CreateOrGetByIdempotencyKey() error = %v", err)
	}
	if created {
		t.Fatal("repeated CreateOrGetByIdempotencyKey() created = true, want false")
	}

	assertTaskEqual(t, first, got)
}

func TestTaskRepository_CreateOrGetByIdempotencyKey_SameKeyDifferentPayload(t *testing.T) {
	db := openTestDB(t)
	clientID := createTestClient(t, db)
	repo := NewTaskRepository(db)
	expected := newTestTask(clientID)

	first, created, err := repo.CreateOrGetByIdempotencyKey(t.Context(), expected)
	if err != nil {
		t.Fatalf("first CreateOrGetByIdempotencyKey() error = %v", err)
	}
	if !created {
		t.Fatal("first CreateOrGetByIdempotencyKey() created = false, want true")
	}

	retry := expected
	retry.ID = uuid.New()
	retry.Payload = json.RawMessage(`{"user_id":99}`)
	retry.RequestHash = "different"

	got, created, err := repo.CreateOrGetByIdempotencyKey(t.Context(), retry)
	if err != nil {
		t.Fatalf("retry CreateOrGetByIdempotencyKey() error = %v", err)
	}
	if created {
		t.Fatal("retry CreateOrGetByIdempotencyKey() created = true, want false")
	}

	assertTaskEqual(t, first, got)
}

func TestTaskRepository_CreateOrGetByIdempotencyKey_AllowsSameKeyForDifferentClients(t *testing.T) {
	db := openTestDB(t)
	firstClientID := createTestClient(t, db)
	secondClientID := createTestClient(t, db)
	repo := NewTaskRepository(db)

	firstTask := newTestTask(firstClientID)
	secondTask := newTestTask(secondClientID)
	secondTask.IdempotencyKey = firstTask.IdempotencyKey

	first, firstCreated, err := repo.CreateOrGetByIdempotencyKey(t.Context(), firstTask)
	if err != nil {
		t.Fatalf("first CreateOrGetByIdempotencyKey() error = %v", err)
	}
	if !firstCreated {
		t.Fatal("first CreateOrGetByIdempotencyKey() created = false, want true")
	}

	second, secondCreated, err := repo.CreateOrGetByIdempotencyKey(t.Context(), secondTask)
	if err != nil {
		t.Fatalf("second CreateOrGetByIdempotencyKey() error = %v", err)
	}
	if !secondCreated {
		t.Fatal("second CreateOrGetByIdempotencyKey() created = false, want true")
	}

	assertTaskEqual(t, firstTask, first)
	assertTaskEqual(t, secondTask, second)

	if first.ID == second.ID {
		t.Fatal("tasks for different clients have the same ID")
	}
}

func TestTaskRepository_CreateOrGetByIdempotencyKey_WithConcurrency(t *testing.T) {
	db := openTestDB(t)
	clientID := createTestClient(t, db)
	repo := NewTaskRepository(db)
	expected := newTestTask(clientID)

	const goroutineCount = 100

	var (
		wg           sync.WaitGroup
		createdCount atomic.Int64
	)

	wg.Add(goroutineCount)

	for range goroutineCount {
		go func() {
			defer wg.Done()

			got, created, err := repo.CreateOrGetByIdempotencyKey(t.Context(), expected)
			if err != nil {
				t.Errorf("CreateOrGetByIdempotencyKey() error = %v", err)
				return
			}
			if created {
				createdCount.Add(1)
			}
			if got.ID != expected.ID {
				t.Errorf("task ID = %s, want %s", got.ID, expected.ID)
			}
		}()
	}

	wg.Wait()

	if got := createdCount.Load(); got != 1 {
		t.Errorf("number of created tasks = %d, want 1", got)
	}

	var count int
	err := db.QueryRow(
		t.Context(),
		"SELECT COUNT(*) FROM tasks WHERE idempotency_key = $1 AND client_id = $2",
		expected.IdempotencyKey,
		clientID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 1 {
		t.Errorf("number of stored tasks = %d, want 1", count)
	}
}

func TestTaskRepository_GetByID(t *testing.T) {
	db := openTestDB(t)
	clientID := createTestClient(t, db)
	repo := NewTaskRepository(db)
	expected := newTestTask(clientID)

	if _, _, err := repo.CreateOrGetByIdempotencyKey(t.Context(), expected); err != nil {
		t.Fatalf("CreateOrGetByIdempotencyKey() error = %v", err)
	}

	got, err := repo.GetByID(t.Context(), expected.ID, clientID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	assertTaskEqual(t, expected, got)

	_, err = repo.GetByID(t.Context(), uuid.New(), clientID)
	if !errors.Is(err, task.ErrTaskNotFound) {
		t.Errorf("GetByID() error = %v, want %v", err, task.ErrTaskNotFound)
	}
}

func TestTaskRepository_GetByID_ReturnsErrWhenTaskBelongsToAnotherClient(t *testing.T) {
	db := openTestDB(t)
	clientID := createTestClient(t, db)
	otherClientID := createTestClient(t, db)
	repo := NewTaskRepository(db)
	expected := newTestTask(clientID)

	if _, _, err := repo.CreateOrGetByIdempotencyKey(t.Context(), expected); err != nil {
		t.Fatalf("CreateOrGetByIdempotencyKey() error = %v", err)
	}

	_, err := repo.GetByID(t.Context(), expected.ID, otherClientID)
	if !errors.Is(err, task.ErrTaskNotFound) {
		t.Errorf("GetByID() error = %v, want %v", err, task.ErrTaskNotFound)
	}
}

func TestTaskRepository_FindAndClaimForWorker(t *testing.T) {
	db := openTestDB(t)
	clientID := createTestClient(t, db)
	repo := NewTaskRepository(db)

	workerID := uuid.New()
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO workers (
			id, token_hash, protocol_version, capabilities, lease_expires_at
		) VALUES ($1, $2, $3, $4::jsonb, NOW() + INTERVAL '1 hour')`,
		workerID,
		"test-token-hash",
		"1",
		`["email-send"]`,
	)
	if err != nil {
		t.Fatalf("insert worker: %v", err)
	}

	expected := newTestTask(clientID)
	expected.Type = "email-send"

	if _, created, err := repo.CreateOrGetByIdempotencyKey(t.Context(), expected); err != nil {
		t.Fatalf("CreateOrGetByIdempotencyKey() error = %v", err)
	} else if !created {
		t.Fatal("expected task to be created")
	}

	got, err := repo.FindAndClaimForWorker(
		t.Context(),
		workerID,
		[]string{"email-send"},
		30,
	)
	if err != nil {
		t.Fatalf("FindAndClaimForWorker() error = %v", err)
	}

	if got.ID != expected.ID {
		t.Errorf("task ID = %s, want %s", got.ID, expected.ID)
	}
	if got.Status != task.StatusRunning {
		t.Errorf("task status = %q, want %q", got.Status, task.StatusRunning)
	}
	if got.AttemptCount != 1 {
		t.Errorf("attempt count = %d, want 1", got.AttemptCount)
	}
	if got.LeaseOwner != workerID {
		t.Errorf("lease owner = %s, want %s", got.LeaseOwner, workerID)
	}
	if got.LeaseToken == uuid.Nil {
		t.Error("lease token must not be nil")
	}
	if got.LeaseExpiresAt == nil {
		t.Fatal("lease expiration must be set")
	}

	var attemptCount int
	err = db.QueryRow(
		t.Context(),
		`SELECT COUNT(*)
		 FROM task_attempts
		 WHERE task_id = $1 AND worker_id = $2 AND attempt_number = 1`,
		expected.ID,
		workerID,
	).Scan(&attemptCount)
	if err != nil {
		t.Fatalf("count task attempts: %v", err)
	}
	if attemptCount != 1 {
		t.Errorf("task attempt rows = %d, want 1", attemptCount)
	}
}

func TestTaskRepository_FindAndClaimForWorker_NoAvailableTasks(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepository(db)

	_, err := repo.FindAndClaimForWorker(
		t.Context(),
		uuid.New(),
		[]string{"email-send"},
		30,
	)
	if !errors.Is(err, task.ErrNoAvailableTasks) {
		t.Fatalf("error = %v, want %v", err, task.ErrNoAvailableTasks)
	}
}

func openTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	db, err := testutil.OpenPostgres(t, true)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	return db
}

func createTestClient(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()

	clientID, err := testutil.CreateTestClient(t, db)
	if err != nil {
		t.Fatalf("create test client: %v", err)
	}

	return clientID
}

func newTestTask(clientID uuid.UUID) task.Task {
	return task.Task{
		ID:             uuid.New(),
		ClientID:       clientID,
		IdempotencyKey: uuid.NewString(),
		RequestHash:    "456",
		Type:           "email-send",
		Payload:        json.RawMessage(`{"user_id":42}`),
		Status:         task.StatusQueued,
		MaxAttempts:    12,
		TimeoutSeconds: 100,
	}
}

func assertTaskEqual(t *testing.T, want, got task.Task) {
	t.Helper()

	if diff := cmp.Diff(
		want,
		got,
		cmpopts.IgnoreFields(task.Task{}, "CreatedAt", "AvailableAt", "Payload"),
	); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}

	assertJSONEqual(t, want.Payload, got.Payload)
}

func assertJSONEqual(t *testing.T, want, got json.RawMessage) {
	t.Helper()

	var expected, actual any

	decode := func(data json.RawMessage, dst *any) error {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		return decoder.Decode(dst)
	}

	if err := decode(want, &expected); err != nil {
		t.Fatalf("decode expected JSON: %v", err)
	}
	if err := decode(got, &actual); err != nil {
		t.Fatalf("decode actual JSON: %v", err)
	}

	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("JSON mismatch (-want +got):\n%s", diff)
	}
}
