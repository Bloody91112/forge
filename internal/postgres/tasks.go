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

func (r *TaskRepository) GetByID(ctx context.Context, id uuid.UUID) (task.Task, error) {
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
			created_at,
			available_at,
			started_at,
			finished_at
		FROM tasks
		WHERE id = $1
	`

	return scanTask(r.db.QueryRow(ctx, selectQuery, id))
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
			created_at,
			available_at,
			started_at,
			finished_at
		FROM tasks
		WHERE client_id = $1
		  AND idempotency_key = $2
	`

	existingTask, err := scanTask(r.db.QueryRow(
		ctx,
		selectQuery,
		t.ClientID,
		t.IdempotencyKey,
	))
	if err != nil {
		return task.Task{}, false, fmt.Errorf("select existing task: %w", err)
	}

	return existingTask, false, nil
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
