package postgres

import (
	"context"
	"errors"
	"fmt"
	"forge/internal/auth"
	"forge/internal/worker"

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
