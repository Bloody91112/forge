package postgres

import (
	"context"
	"fmt"
	"forge/internal/worker"

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
