package postgres

import (
	"context"
	"errors"
	"fmt"
	"forge/internal/task"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TaskRepository struct {
	db *pgxpool.Pool
}

func NewTaskRepository(db *pgxpool.Pool) *TaskRepository {
	return &TaskRepository{db: db}
}

func (r *TaskRepository) GetByID(ctx context.Context, taskID uuid.UUID, clientID uuid.UUID) (task.Task, error) {
	const selectQuery = `
		SELECT
			id,
			client_id,
			idempotency_key,
			request_hash,
			type,
			payload,
			status,
			result,
			error_code,
			error_message,
			attempt_count,
			max_attempts,
			timeout_seconds,
			lease_owner,
			lease_token,
			lease_expires_at,
			created_at,
			available_at,
			started_at,
			finished_at
		FROM tasks
		WHERE id = $1 AND client_id = $2
	`

	gotTask, err := scanTask(r.db.QueryRow(ctx, selectQuery, taskID, clientID))

	if errors.Is(err, pgx.ErrNoRows) {
		return task.Task{}, task.ErrTaskNotFound
	}

	return gotTask, err
}

func (r *TaskRepository) CreateOrGetByIdempotencyKey(ctx context.Context, t task.Task) (task.Task, bool, error) {
	const insertQuery = `
		INSERT INTO tasks (
			id,
			client_id,
			idempotency_key,
			request_hash,
			type,
			payload,
			status,
			max_attempts,
			timeout_seconds
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (client_id, idempotency_key) DO NOTHING
		RETURNING
			id,
			client_id,
			idempotency_key,
			request_hash,
			type,
			payload,
			status,
			result,
			error_code,
			error_message,
			attempt_count,
			max_attempts,
			timeout_seconds,
		    lease_owner,
			lease_token,
			lease_expires_at,
			created_at,
			available_at,
			started_at,
			finished_at
	`

	createdTask, err := scanTask(r.db.QueryRow(
		ctx,
		insertQuery,
		t.ID,
		t.ClientID,
		t.IdempotencyKey,
		t.RequestHash,
		t.Type,
		[]byte(t.Payload),
		t.Status,
		t.MaxAttempts,
		t.TimeoutSeconds,
	))
	if err == nil {
		return createdTask, true, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return task.Task{}, false, fmt.Errorf("insert task: %w", err)
	}

	existingTask, err := r.GetByIdempotencyKey(ctx, t.ClientID, t.IdempotencyKey)

	return existingTask, false, err
}

func (r *TaskRepository) GetByIdempotencyKey(ctx context.Context, clientID uuid.UUID, key string) (task.Task, error) {
	const selectQuery = `
		SELECT
			id,
			client_id,
			idempotency_key,
			request_hash,
			type,
			payload,
			status,
			result,
			error_code,
			error_message,
			attempt_count,
			max_attempts,
			timeout_seconds,
			lease_owner,
			lease_token,
			lease_expires_at,
			created_at,
			available_at,
			started_at,
			finished_at
		FROM tasks
		WHERE client_id = $1
		  AND idempotency_key = $2
	`

	existingTask, err := scanTask(r.db.QueryRow(ctx, selectQuery, clientID, key))

	if errors.Is(err, pgx.ErrNoRows) {
		return task.Task{}, task.ErrTaskNotFound
	}

	if err != nil {
		return task.Task{}, fmt.Errorf("select existing task: %w", err)
	}

	return existingTask, nil
}

func (r *TaskRepository) FindAndClaimForWorker(ctx context.Context, workerID uuid.UUID, workerCapabilities []string, leaseSeconds int) (task.Task, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return task.Task{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var taskID uuid.UUID
	err = tx.QueryRow(ctx, "SELECT id FROM tasks WHERE status = $1 AND available_at <= NOW() AND type = ANY($2) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1", task.StatusQueued, workerCapabilities).Scan(&taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return task.Task{}, task.ErrNoAvailableTasks
	}
	if err != nil {
		return task.Task{}, fmt.Errorf("find task to claim: %w", err)
	}

	const updateQuery = `
		UPDATE tasks
		SET status = $1,
		    attempt_count = attempt_count + 1,
		    lease_owner = $2,
		    lease_token = gen_random_uuid(),
		    lease_expires_at = NOW() + ($3 * INTERVAL '1 second'),
		    started_at = COALESCE(started_at, NOW())
		WHERE id = $4
		RETURNING
		    id, 
		    client_id, 
		    idempotency_key, 
		    request_hash, 
		    type, 
		    payload,
		    status, 
		    result, 
		    error_code, 
		    error_message, 
		    attempt_count,
		    max_attempts, 
		    timeout_seconds, 
		    lease_owner, 
		    lease_token, 
		    lease_expires_at,
		    created_at, 
		    available_at,
		    started_at, 
		    finished_at
	`

	claimedTask, err := scanTask(tx.QueryRow(ctx, updateQuery, task.StatusRunning, workerID, leaseSeconds, taskID))
	if err != nil {
		return task.Task{}, fmt.Errorf("claim task: %w", err)
	}

	_, err = tx.Exec(ctx, "INSERT INTO task_attempts (id, task_id, attempt_number, worker_id, started_at) VALUES (gen_random_uuid(), $1, $2, $3, NOW())", claimedTask.ID, claimedTask.AttemptCount, workerID)
	if err != nil {
		return task.Task{}, fmt.Errorf("create task attempt: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return task.Task{}, fmt.Errorf("commit claim transaction: %w", err)
	}

	return claimedTask, nil
}

func scanTask(row pgx.Row) (task.Task, error) {
	var t task.Task
	var payload, result []byte

	err := row.Scan(
		&t.ID,
		&t.ClientID,
		&t.IdempotencyKey,
		&t.RequestHash,
		&t.Type,
		&payload,
		&t.Status,
		&result,
		&t.ErrorCode,
		&t.ErrorMessage,
		&t.AttemptCount,
		&t.MaxAttempts,
		&t.TimeoutSeconds,
		&t.LeaseOwner,
		&t.LeaseToken,
		&t.LeaseExpiresAt,
		&t.CreatedAt,
		&t.AvailableAt,
		&t.StartedAt,
		&t.FinishedAt,
	)
	if err != nil {
		return task.Task{}, err
	}

	t.Payload = payload

	if result != nil {
		t.Result = result
	}

	return t, nil
}
