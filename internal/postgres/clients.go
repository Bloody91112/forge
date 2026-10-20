package postgres

import (
	"context"
	"errors"
	"fmt"
	"forge/internal/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ClientRepository struct {
	db *pgxpool.Pool
}

func NewClientRepository(db *pgxpool.Pool) *ClientRepository {
	return &ClientRepository{db: db}
}

func (r *ClientRepository) GetClientIDByTokenHash(ctx context.Context, hash string) (uuid.UUID, error) {
	var clientID uuid.UUID

	err := r.db.QueryRow(ctx, "SELECT id FROM clients WHERE token_hash = $1", hash).Scan(&clientID)

	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, auth.ErrClientNotFound
	}

	if err != nil {
		return uuid.Nil, fmt.Errorf("get client by token hash: %w", err)
	}

	return clientID, nil
}
