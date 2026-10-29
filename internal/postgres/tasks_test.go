package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"forge/internal/task"
	"forge/internal/testutil"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func TestTaskRepository_FindAndClaimForWorker_Concurrent(t *testing.T) {
	db := openTestDB(t)
	clientID := createTestClient(t, db)
	repo := NewTaskRepository(db)

	workerIDs := []uuid.UUID{uuid.New(), uuid.New()}
	for _, workerID := range workerIDs {
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
	}

	firstTask := newTestTask(clientID)
	secondTask := newTestTask(clientID)

	for _, taskToCreate := range []task.Task{firstTask, secondTask} {
		if _, created, err := repo.CreateOrGetByIdempotencyKey(
			t.Context(), taskToCreate,
		); err != nil {
			t.Fatalf("create task: %v", err)
		} else if !created {
			t.Fatal("expected task to be created")
		}
	}

	type claimResult struct {
		task task.Task
		err  error
	}

	start := make(chan struct{})
	results := make(chan claimResult, len(workerIDs))

	for _, workerID := range workerIDs {
		go func(workerID uuid.UUID) {
			<-start

			claimed, err := repo.FindAndClaimForWorker(
				t.Context(),
				workerID,
				[]string{"email-send"},
				30,
			)
			results <- claimResult{task: claimed, err: err}
		}(workerID)
	}

	close(start)

	claimedIDs := make(map[uuid.UUID]bool)
	for range workerIDs {
		result := <-results
		if result.err != nil {
			t.Fatalf("FindAndClaimForWorker() error = %v", result.err)
		}

		if result.task.Status != task.StatusRunning {
			t.Errorf("task status = %q, want %q",
				result.task.Status, task.StatusRunning)
		}
		if claimedIDs[result.task.ID] {
			t.Errorf("task %s was claimed more than once", result.task.ID)
		}
		claimedIDs[result.task.ID] = true
	}

	if len(claimedIDs) != 2 {
		t.Fatalf("number of distinct claimed tasks = %d, want 2", len(claimedIDs))
	}

	var attemptCount int
	err := db.QueryRow(
		t.Context(),
		`SELECT COUNT(*)
		 FROM task_attempts
		 WHERE task_id IN ($1, $2)`,
		firstTask.ID,
		secondTask.ID,
	).Scan(&attemptCount)
	if err != nil {
		t.Fatalf("count task attempts: %v", err)
	}
	if attemptCount != 2 {
		t.Errorf("task attempt rows = %d, want 2", attemptCount)
	}
}

func TestTaskRepository_FindAndClaimForWorker_RepeatedClaim(t *testing.T) {
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
	if _, created, err := repo.CreateOrGetByIdempotencyKey(
		t.Context(), expected,
	); err != nil {
		t.Fatalf("create task: %v", err)
	} else if !created {
		t.Fatal("expected task to be created")
	}

	first, err := repo.FindAndClaimForWorker(
		t.Context(), workerID, []string{"email-send"}, 30,
	)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if first.AttemptCount != 1 {
		t.Fatalf("first attempt count = %d, want 1", first.AttemptCount)
	}

	// Повторно делаем задачу доступной только для подготовки теста.
	_, err = db.Exec(
		t.Context(),
		`UPDATE tasks
		 SET status = 'queued',
		     lease_owner = NULL,
		     lease_token = NULL,
		     lease_expires_at = NULL,
		     available_at = NOW()
		 WHERE id = $1`,
		expected.ID,
	)
	if err != nil {
		t.Fatalf("reset task for second claim: %v", err)
	}

	second, err := repo.FindAndClaimForWorker(
		t.Context(), workerID, []string{"email-send"}, 30,
	)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("second claimed task ID = %s, want %s", second.ID, first.ID)
	}
	if second.AttemptCount != 2 {
		t.Errorf("second attempt count = %d, want 2", second.AttemptCount)
	}

	var attemptCount int
	err = db.QueryRow(
		t.Context(),
		`SELECT COUNT(*)
		 FROM task_attempts
		 WHERE task_id = $1 AND worker_id = $2`,
		expected.ID,
		workerID,
	).Scan(&attemptCount)
	if err != nil {
		t.Fatalf("count task attempts: %v", err)
	}
	if attemptCount != 2 {
		t.Errorf("task attempt rows = %d, want 2", attemptCount)
	}

	var attemptNumbers []int
	rows, err := db.Query(
		t.Context(),
		`SELECT attempt_number
		 FROM task_attempts
		 WHERE task_id = $1
		 ORDER BY attempt_number`,
		expected.ID,
	)
	if err != nil {
		t.Fatalf("query task attempts: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var number int
		if err := rows.Scan(&number); err != nil {
			t.Fatalf("scan attempt number: %v", err)
		}
		attemptNumbers = append(attemptNumbers, number)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate task attempts: %v", err)
	}

	if len(attemptNumbers) != 2 ||
		attemptNumbers[0] != 1 ||
		attemptNumbers[1] != 2 {
		t.Errorf("attempt numbers = %v, want [1 2]", attemptNumbers)
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

func createRunningTask(
	t *testing.T,
	db *pgxpool.Pool,
	repo *TaskRepository,
) (task.Task, uuid.UUID, uuid.UUID) {
	t.Helper()

	clientID := createTestClient(t, db)
	expected := newTestTask(clientID)

	if _, created, err := repo.CreateOrGetByIdempotencyKey(
		t.Context(), expected,
	); err != nil {
		t.Fatalf("create task: %v", err)
	} else if !created {
		t.Fatal("expected task to be created")
	}

	workerID := uuid.New()
	leaseToken := uuid.New()

	_, err := db.Exec(t.Context(), `
    INSERT INTO workers (id, token_hash, protocol_version, capabilities, lease_expires_at)
    VALUES ($1, $2, $3, $4::jsonb, clock_timestamp() + INTERVAL '1 hour')
`, workerID, "test-token-hash", "1", `["email-send"]`)
	if err != nil {
		t.Fatalf("insert worker: %v", err)
	}

	_, err = db.Exec(t.Context(), `
		UPDATE tasks
		SET status = 'running',
		    attempt_count = 1,
		    lease_owner = $2,
		    lease_token = $3,
		    lease_expires_at = clock_timestamp() + INTERVAL '5 minutes'
		WHERE id = $1
	`, expected.ID, workerID, leaseToken)
	if err != nil {
		t.Fatalf("prepare running task: %v", err)
	}

	_, err = db.Exec(t.Context(), `
		INSERT INTO task_attempts (id, task_id, attempt_number, worker_id, started_at)
		VALUES (gen_random_uuid(), $1, 1, $2, clock_timestamp())
	`, expected.ID, workerID)
	if err != nil {
		t.Fatalf("create task attempt: %v", err)
	}

	return expected, workerID, leaseToken
}

func TestTaskRepository_RenewTaskLeaseForWorker(t *testing.T) {
	t.Run("renews lease", func(t *testing.T) {
		db := openTestDB(t)
		repo := NewTaskRepository(db)
		expected, workerID, token := createRunningTask(t, db, repo)

		_, err := db.Exec(t.Context(),
			`UPDATE tasks
			 SET lease_expires_at = clock_timestamp() + INTERVAL '30 seconds'
			 WHERE id = $1`,
			expected.ID,
		)
		if err != nil {
			t.Fatalf("set initial lease expiration: %v", err)
		}

		var oldExpiry time.Time
		err = db.QueryRow(t.Context(),
			`SELECT lease_expires_at FROM tasks WHERE id = $1`,
			expected.ID,
		).Scan(&oldExpiry)
		if err != nil {
			t.Fatal(err)
		}

		gotExpiry, err := repo.RenewTaskLeaseForWorker(
			t.Context(), expected.ID, workerID, token, 120,
		)
		if err != nil {
			t.Fatalf("RenewTaskLeaseForWorker(): %v", err)
		}
		if !gotExpiry.After(oldExpiry) {
			t.Errorf("new expiry %v should be after old expiry %v",
				gotExpiry, oldExpiry)
		}

		var storedExpiry time.Time
		err = db.QueryRow(t.Context(),
			`SELECT lease_expires_at FROM tasks WHERE id = $1`,
			expected.ID,
		).Scan(&storedExpiry)
		if err != nil {
			t.Fatal(err)
		}
		if !storedExpiry.Equal(gotExpiry) {
			t.Errorf("stored expiry = %v, returned expiry = %v",
				storedExpiry, gotExpiry)
		}
	})

	t.Run("rejects wrong token", func(t *testing.T) {
		db := openTestDB(t)
		repo := NewTaskRepository(db)
		expected, workerID, _ := createRunningTask(t, db, repo)

		_, err := repo.RenewTaskLeaseForWorker(
			t.Context(), expected.ID, workerID, uuid.New(), 120,
		)
		if !errors.Is(err, task.ErrTaskLeaseLost) {
			t.Fatalf("error = %v, want %v", err, task.ErrTaskLeaseLost)
		}
	})
}

func TestTaskRepository_MarkTaskAsCompleted(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepository(db)
	expected, workerID, token := createRunningTask(t, db, repo)
	result := json.RawMessage(`{"ok":true}`)

	err := repo.MarkTaskAsCompleted(
		t.Context(), expected.ID, workerID, token, result,
	)
	if err != nil {
		t.Fatalf("MarkTaskAsCompleted(): %v", err)
	}

	var status string
	var storedResult []byte
	var leaseOwner, leaseToken *uuid.UUID
	var leaseExpiry *time.Time

	err = db.QueryRow(t.Context(), `
		SELECT status, result, lease_owner, lease_token, lease_expires_at
		FROM tasks WHERE id = $1
	`, expected.ID).Scan(
		&status, &storedResult, &leaseOwner, &leaseToken, &leaseExpiry,
	)
	if err != nil {
		t.Fatal(err)
	}

	if status != "succeeded" {
		t.Errorf("status = %q, want succeeded", status)
	}
	assertJSONEqual(t, result, storedResult)

	if leaseOwner != nil || leaseToken != nil || leaseExpiry != nil {
		t.Error("lease fields should be cleared after completion")
	}

	var outcome string
	var finishedAt *time.Time
	err = db.QueryRow(t.Context(), `
		SELECT outcome, finished_at
		FROM task_attempts
		WHERE task_id = $1 AND attempt_number = 1
	`, expected.ID).Scan(&outcome, &finishedAt)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != "succeeded" {
		t.Errorf("attempt outcome = %q, want succeeded", outcome)
	}
	if finishedAt == nil {
		t.Error("attempt finished_at should be set")
	}
}

func TestTaskRepository_MarkTaskAsCompleted_RejectsWrongToken(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepository(db)
	expected, workerID, _ := createRunningTask(t, db, repo)

	err := repo.MarkTaskAsCompleted(
		t.Context(), expected.ID, workerID, uuid.New(),
		json.RawMessage(`{"ok":true}`),
	)
	if !errors.Is(err, task.ErrTaskLeaseLost) {
		t.Fatalf("error = %v, want %v", err, task.ErrTaskLeaseLost)
	}

	var status string
	if err := db.QueryRow(t.Context(),
		`SELECT status FROM tasks WHERE id = $1`, expected.ID,
	).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "running" {
		t.Errorf("status = %q, want running", status)
	}
}

func TestTaskRepository_MarkTaskAsFailed(t *testing.T) {
	tests := []struct {
		name        string
		retryable   bool
		wantStatus  string
		wantOutcome string
	}{
		{
			name:        "retryable failure requeues task",
			retryable:   true,
			wantStatus:  "queued",
			wantOutcome: "retryable_error",
		},
		{
			name:        "permanent failure finishes task",
			retryable:   false,
			wantStatus:  "failed",
			wantOutcome: "permanent_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openTestDB(t)
			repo := NewTaskRepository(db)
			expected, workerID, token := createRunningTask(t, db, repo)

			err := repo.MarkTaskAsFailed(
				t.Context(), expected.ID, workerID, token,
				"worker_error", "processing failed", tt.retryable,
			)
			if err != nil {
				t.Fatalf("MarkTaskAsFailed(): %v", err)
			}

			var status, errorCode, errorMessage string
			err = db.QueryRow(t.Context(), `
				SELECT status, error_code, error_message
				FROM tasks WHERE id = $1
			`, expected.ID).Scan(&status, &errorCode, &errorMessage)
			if err != nil {
				t.Fatal(err)
			}

			if status != tt.wantStatus {
				t.Errorf("status = %q, want %q", status, tt.wantStatus)
			}
			if errorCode != "worker_error" {
				t.Errorf("error_code = %q, want worker_error", errorCode)
			}
			if errorMessage != "processing failed" {
				t.Errorf("error_message = %q, want processing failed", errorMessage)
			}

			var outcome string
			var finishedAt *time.Time
			err = db.QueryRow(t.Context(), `
				SELECT outcome, finished_at
				FROM task_attempts
				WHERE task_id = $1 AND attempt_number = 1
			`, expected.ID).Scan(&outcome, &finishedAt)
			if err != nil {
				t.Fatal(err)
			}
			if outcome != tt.wantOutcome {
				t.Errorf("attempt outcome = %q, want %q",
					outcome, tt.wantOutcome)
			}
			if finishedAt == nil {
				t.Error("attempt finished_at should be set")
			}
		})
	}
}

func TestTaskRepository_MarkTaskAsFailed_RejectsWrongToken(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepository(db)
	expected, workerID, _ := createRunningTask(t, db, repo)

	err := repo.MarkTaskAsFailed(
		t.Context(), expected.ID, workerID, uuid.New(),
		"worker_error", "processing failed", false,
	)
	if !errors.Is(err, task.ErrTaskLeaseLost) {
		t.Fatalf("error = %v, want %v", err, task.ErrTaskLeaseLost)
	}
}

func TestTaskRepository_LeaseExpiresWhileWaitingForRowLock(t *testing.T) {
	tests := []struct {
		name         string
		queryPattern string
		run          func(
			context.Context,
			*TaskRepository,
			task.Task,
			uuid.UUID,
			uuid.UUID,
		) error
	}{
		{
			name:         "renew",
			queryPattern: "%SET lease_expires_at = clock_timestamp()%",
			run: func(ctx context.Context, repo *TaskRepository, taskToTest task.Task, workerID, token uuid.UUID) error {
				_, err := repo.RenewTaskLeaseForWorker(
					ctx, taskToTest.ID, workerID, token, 120,
				)
				return err
			},
		},
		{
			name:         "complete",
			queryPattern: "%SET status = 'succeeded'%",
			run: func(ctx context.Context, repo *TaskRepository, taskToTest task.Task, workerID, token uuid.UUID) error {
				return repo.MarkTaskAsCompleted(
					ctx, taskToTest.ID, workerID, token,
					json.RawMessage(`{"ok":true}`),
				)
			},
		},
		{
			name:         "fail",
			queryPattern: "%SELECT attempt_count, max_attempts%",
			run: func(ctx context.Context, repo *TaskRepository, taskToTest task.Task, workerID, token uuid.UUID) error {
				return repo.MarkTaskAsFailed(
					ctx, taskToTest.ID, workerID, token,
					"worker_error", "processing failed", false,
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openTestDB(t)
			repo := NewTaskRepository(db)
			expected, workerID, token := createRunningTask(t, db, repo)

			lockTx, err := db.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer lockTx.Rollback(context.Background())

			if _, err := lockTx.Exec(t.Context(), `
				SELECT id FROM tasks WHERE id = $1 FOR UPDATE
			`, expected.ID); err != nil {
				t.Fatal(err)
			}

			var expiresAt time.Time
			err = lockTx.QueryRow(t.Context(), `
				UPDATE tasks
				SET lease_expires_at = clock_timestamp() + INTERVAL '1 second'
				WHERE id = $1
				RETURNING lease_expires_at
			`, expected.ID).Scan(&expiresAt)
			if err != nil {
				t.Fatal(err)
			}

			result := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				result <- tt.run(ctx, repo, expected, workerID, token)
			}()

			// Ждём подтверждения, что запрос действительно заблокирован на строке.
			deadline := time.Now().Add(3 * time.Second)
			for {
				var blocked bool
				err := db.QueryRow(t.Context(), `
					SELECT EXISTS (
						SELECT 1
						FROM pg_stat_activity
						WHERE datname = current_database()
						  AND wait_event_type = 'Lock'
						  AND query LIKE $1
					)
				`, tt.queryPattern).Scan(&blocked)
				if err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("operation did not block on the row lock")
				}
				time.Sleep(10 * time.Millisecond)
			}

			if wait := time.Until(expiresAt) + 50*time.Millisecond; wait > 0 {
				time.Sleep(wait)
			}

			if err := lockTx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}

			if err := <-result; !errors.Is(err, task.ErrTaskLeaseLost) {
				t.Fatalf("error = %v, want %v", err, task.ErrTaskLeaseLost)
			}

			var status string
			if err := db.QueryRow(t.Context(),
				`SELECT status FROM tasks WHERE id = $1`, expected.ID,
			).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != "running" {
				t.Errorf("status = %q, want running", status)
			}
		})
	}
}
