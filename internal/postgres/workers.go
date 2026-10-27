package postgres

import (
	"context"
	"errors"
	"fmt"
	"forge/internal/auth"
	"forge/internal/worker"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WorkerRepository struct {
	db *pgxpool.Pool
}

func NewWorkerRepository(db *pgxpool.Pool) *WorkerRepository {
	return &WorkerRepository{db: db}
}

func (r *WorkerRepository) Register(ctx context.Context, worker worker.Worker) error {
	query := `
    INSERT INTO workers (
        id,
        token_hash,
        protocol_version,
        capabilities,
        registered_at,
        last_seen_at,
        lease_expires_at
    )
    VALUES ($1, $2, $3, $4, $5, NULL, $6)
    ON CONFLICT (id) DO UPDATE SET
        token_hash = EXCLUDED.token_hash,
        protocol_version = EXCLUDED.protocol_version,
        capabilities = EXCLUDED.capabilities,
        registered_at = EXCLUDED.registered_at,
        last_seen_at = NULL,
        lease_expires_at = EXCLUDED.lease_expires_at
	`
	_, err := r.db.Exec(
		ctx,
		query,
		worker.ID,
		worker.TokenHash,
		worker.ProtocolVersion,
		worker.Capabilities,
		worker.RegisteredAt,
		worker.LeaseExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("register worker: %w", err)
	}

	return nil
}

func (r *WorkerRepository) GetWorkerByTokenHash(ctx context.Context, hash string) (worker.Worker, error) {
	var foundWorker worker.Worker
	query := "SELECT id, token_hash, protocol_version, capabilities, registered_at, last_seen_at, lease_expires_at FROM workers WHERE token_hash = $1"

	err := r.db.QueryRow(
		ctx,
		query,
		hash,
	).Scan(&foundWorker.ID, &foundWorker.TokenHash, &foundWorker.ProtocolVersion, &foundWorker.Capabilities, &foundWorker.RegisteredAt, &foundWorker.LastSeenAt, &foundWorker.LeaseExpiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return worker.Worker{}, auth.ErrWorkerNotFound
	}

	if err != nil {
		return worker.Worker{}, fmt.Errorf("get worker by token hash: %w", err)
	}

	return foundWorker, nil
}

func (r *WorkerRepository) ExtendLease(ctx context.Context, id uuid.UUID, leaseExpiresAt time.Time, seenAt time.Time) error {
	query := "UPDATE workers SET lease_expires_at = $1, last_seen_at = $2 WHERE id = $3"
	tag, err := r.db.Exec(ctx, query, leaseExpiresAt, seenAt, id)

	if err != nil {
		return fmt.Errorf("extend worker lease: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return worker.ErrWorkerNotFound
	}

	return nil
}

func (r *WorkerRepository) GetActiveWorkersCapabilities(ctx context.Context) ([][]string, error) {
	rows, err := r.db.Query(ctx, "SELECT capabilities FROM workers WHERE lease_expires_at > NOW()")
	if err != nil {
		return nil, fmt.Errorf("get active worker capabilities: %w", err)
	}
	defer rows.Close()

	var capabilities [][]string

	for rows.Next() {
		var workerCapabilities []string

		if err := rows.Scan(&workerCapabilities); err != nil {
			return nil, fmt.Errorf("scan worker capabilities: %w", err)
		}

		capabilities = append(capabilities, workerCapabilities)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate worker capabilities: %w", err)
	}

	return capabilities, nil
}
