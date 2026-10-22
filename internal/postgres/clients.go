package postgres

import (
	"context"
	"errors"
	"fmt"
	"forge/internal/auth"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ClientRepository struct {
	db *pgxpool.Pool
}

func NewClientRepository(db *pgxpool.Pool) *ClientRepository {
	return &ClientRepository{db: db}
}

func (r *ClientRepository) GetClientByTokenHash(ctx context.Context, hash string) (auth.Client, error) {
	var client auth.Client

	err := r.db.QueryRow(ctx, "SELECT id, name, token_hash, created_at FROM clients WHERE token_hash = $1", hash).Scan(&client.ID, &client.Name, &client.TokenHash, &client.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Client{}, auth.ErrClientNotFound
	}

	if err != nil {
		return auth.Client{}, fmt.Errorf("get client by token hash: %w", err)
	}

	return client, nil
}

func (r *ClientRepository) CreateClient(ctx context.Context, client auth.Client) (auth.Client, error) {
	var createdClient auth.Client

	query := "INSERT INTO clients (id, name, token_hash) values ($1, $2, $3) RETURNING id, name, token_hash, created_at"
	err := r.db.QueryRow(
		ctx,
		query,
		client.ID, client.Name, client.TokenHash,
	).Scan(&createdClient.ID, &createdClient.Name, &createdClient.TokenHash, &createdClient.CreatedAt)

	if err != nil {
		return auth.Client{}, fmt.Errorf("create client: %w", err)
	}

	return createdClient, nil
}
