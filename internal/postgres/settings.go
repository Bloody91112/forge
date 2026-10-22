package postgres

import (
	"context"
	"errors"
	"fmt"
	"forge/internal/settings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SettingRepository struct {
	db *pgxpool.Pool
}

func NewSettingRepository(db *pgxpool.Pool) *SettingRepository {
	return &SettingRepository{db: db}
}

func (r *SettingRepository) Get(ctx context.Context, key string) (string, error) {
	var settingValue string
	err := r.db.QueryRow(ctx, "SELECT value FROM forge_settings WHERE key = $1", key).Scan(&settingValue)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", settings.ErrSettingNotFound
	}

	if err != nil {
		return "", fmt.Errorf("get setting: %w", err)
	}

	return settingValue, nil
}
func (r *SettingRepository) Create(ctx context.Context, key string, value string) error {
	_, err := r.db.Exec(ctx, "INSERT INTO forge_settings (key, value) values ($1, $2)", key, value)

	if err != nil {
		return fmt.Errorf("create setting: %w", err)
	}

	return nil

}
func (r *SettingRepository) Update(ctx context.Context, key string, value string) error {
	tag, err := r.db.Exec(ctx, "UPDATE forge_settings SET value = $2, updated_at = CURRENT_TIMESTAMP WHERE key = $1", key, value)

	if err != nil {
		return fmt.Errorf("update setting: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return settings.ErrSettingNotFound
	}

	return nil
}
