package postgres

import (
	"context"
	"errors"
	"fmt"
	"forge/internal/auth"
	"time"

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

func (r *ClientRepository) GetClientByTokenHash(ctx context.Context, hash string) (auth.Client, error) {
	var client auth.Client

	query := `
		SELECT id, name, token_hash, created_at
		FROM clients
		WHERE token_hash = $1
		   OR (
				previous_token_hash = $1
				AND previous_token_expires_at > clock_timestamp()
		   )
	`

	err := r.db.QueryRow(ctx, query, hash).Scan(
		&client.ID,
		&client.Name,
		&client.TokenHash,
		&client.CreatedAt,
	)

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

	query := `
		INSERT INTO clients (id, name, token_hash)
		VALUES ($1, $2, $3)
		RETURNING id, name, token_hash, created_at
	`

	err := r.db.QueryRow(
		ctx,
		query,
		client.ID,
		client.Name,
		client.TokenHash,
	).Scan(
		&createdClient.ID,
		&createdClient.Name,
		&createdClient.TokenHash,
		&createdClient.CreatedAt,
	)

	if err != nil {
		return auth.Client{}, fmt.Errorf("create client: %w", err)
	}

	return createdClient, nil
}

func (r *ClientRepository) RotateClientToken(ctx context.Context, id uuid.UUID, tokenHash string) (auth.Client, error) {
	var client auth.Client
	var previousTokenHash string
	var previousTokenExpiresAt time.Time

	query := `
		UPDATE clients
		SET previous_token_hash = token_hash,
			previous_token_expires_at = clock_timestamp() + INTERVAL '5 minutes',
			token_hash = $2
		WHERE id = $1
		RETURNING
			id,
			name,
			token_hash,
			created_at,
			previous_token_hash,
			previous_token_expires_at
	`

	err := r.db.QueryRow(ctx, query, id, tokenHash).Scan(
		&client.ID,
		&client.Name,
		&client.TokenHash,
		&client.CreatedAt,
		&previousTokenHash,
		&previousTokenExpiresAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Client{}, auth.ErrClientNotFound
	}

	if err != nil {
		return auth.Client{}, fmt.Errorf("rotate client token: %w", err)
	}

	client.PreviousTokenHash = &previousTokenHash
	client.PreviousTokenExpiresAt = &previousTokenExpiresAt

	return client, nil
}
