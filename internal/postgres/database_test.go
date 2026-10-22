package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"forge/internal/config"
	"forge/internal/database"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func getTestClientID(t *testing.T, db *pgxpool.Pool) (uuid.UUID, error) {
	var clientId uuid.UUID
	tokenHash := rand.Text()

	err := db.QueryRow(t.Context(), "INSERT INTO clients (id, name, token_hash) VALUES ($1, 'BOB', $2) RETURNING id", uuid.New(), tokenHash).Scan(&clientId)
	return clientId, err
}

func getTestDb(t *testing.T, cleanBeforeTest bool) (*pgxpool.Pool, error) {
	_ = godotenv.Load("../../.env")

	dbName := os.Getenv("DB_DATABASE")
	testDbName := os.Getenv("DB_TEST_DATABASE")
	if testDbName == "" {
		return nil, errors.New("env DB_TEST_DATABASE not specified")
	}

	if dbName == "" {
		return nil, errors.New("env DB_DATABASE not specified")
	}

	if dbName == testDbName {
		return nil, errors.New("env DB_TEST_DATABASE equals DB_DATABASE")
	}

	t.Setenv("DB_DATABASE", testDbName)

	conf, err := config.Load()
	if err != nil {
		return nil, err
	}

	pool, err := database.Open(t.Context(), conf)
	if err != nil {
		return nil, err
	}

	t.Cleanup(func() {
		pool.Close()
	})

	pingCtx, pingCancel := context.WithTimeout(t.Context(), 1*time.Second)
	err = pool.Ping(pingCtx)
	pingCancel()

	if err != nil {
		return nil, fmt.Errorf("ping postgres, %w", err)
	}

	var connectedDbName string
	err = pool.QueryRow(t.Context(), "SELECT current_database()").Scan(&connectedDbName)
	if err != nil {
		return nil, fmt.Errorf("selecting current db: %w", err)
	}

	if connectedDbName != testDbName {
		return nil, fmt.Errorf("wrong database connected: expected %q, got %q", testDbName, connectedDbName)
	}

	if cleanBeforeTest {
		_, err := pool.Exec(t.Context(), "TRUNCATE TABLE tasks, clients, forge_settings CASCADE;")
		if err != nil {
			return nil, fmt.Errorf("failed to truncate test db: %w", err)
		}
		log.Println("test db successfully truncated")
	} else {
		log.Println("test db cleanup option is not selected")
	}

	return pool, nil
}
