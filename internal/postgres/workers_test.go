package postgres

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"forge/internal/testutil"
	"forge/internal/worker"

	"github.com/google/uuid"
)

func TestWorkerRepository_RegisterAndReplaceExistingWorker(t *testing.T) {
	db, err := testutil.OpenPostgres(t, true)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	repo := NewWorkerRepository(db)
	workerID := uuid.New()
	registeredAt := time.Now().UTC().Truncate(time.Microsecond)

	first := worker.Worker{
		ID:              workerID,
		TokenHash:       "first-token-hash",
		ProtocolVersion: "1",
		Capabilities:    []string{"email"},
		RegisteredAt:    registeredAt,
		LeaseExpiresAt:  registeredAt.Add(30 * time.Second),
	}

	if err := repo.Register(t.Context(), first); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	var (
		tokenHash      string
		protocol       string
		capabilities   []byte
		storedRegister time.Time
		lastSeenAt     sql.NullTime
		leaseExpiresAt time.Time
	)

	err = db.QueryRow(
		t.Context(),
		`SELECT token_hash, protocol_version, capabilities, registered_at, last_seen_at, lease_expires_at FROM workers WHERE id = $1`,
		workerID,
	).Scan(
		&tokenHash,
		&protocol,
		&capabilities,
		&storedRegister,
		&lastSeenAt,
		&leaseExpiresAt,
	)
	if err != nil {
		t.Fatalf("read registered worker: %v", err)
	}

	if tokenHash != first.TokenHash {
		t.Fatalf("token hash = %q, want %q", tokenHash, first.TokenHash)
	}
	if protocol != first.ProtocolVersion {
		t.Fatalf("protocol version = %q, want %q", protocol, first.ProtocolVersion)
	}
	if !storedRegister.Equal(first.RegisteredAt) {
		t.Fatalf("registered_at = %v, want %v", storedRegister, first.RegisteredAt)
	}
	if lastSeenAt.Valid {
		t.Fatalf("last_seen_at = %v, want NULL", lastSeenAt.Time)
	}
	if !leaseExpiresAt.Equal(first.LeaseExpiresAt) {
		t.Fatalf("lease_expires_at = %v, want %v", leaseExpiresAt, first.LeaseExpiresAt)
	}

	var gotCapabilities []string
	if err := json.Unmarshal(capabilities, &gotCapabilities); err != nil {
		t.Fatalf("decode stored capabilities: %v", err)
	}
	if len(gotCapabilities) != 1 || gotCapabilities[0] != "email" {
		t.Fatalf("stored capabilities = %v, want [email]", gotCapabilities)
	}

	lastSeen := registeredAt.Add(5 * time.Second)
	if _, err := db.Exec(
		t.Context(),
		"UPDATE workers SET last_seen_at = $2 WHERE id = $1",
		workerID,
		lastSeen,
	); err != nil {
		t.Fatalf("set last_seen_at for replacement scenario: %v", err)
	}

	replacementAt := registeredAt.Add(time.Minute)
	replacement := worker.Worker{
		ID:              workerID,
		TokenHash:       "replacement-token-hash",
		ProtocolVersion: "1",
		Capabilities:    []string{"webhook", "email"},
		RegisteredAt:    replacementAt,
		LeaseExpiresAt:  replacementAt.Add(30 * time.Second),
	}

	if err := repo.Register(t.Context(), replacement); err != nil {
		t.Fatalf("re-register worker: %v", err)
	}

	err = db.QueryRow(
		t.Context(),
		`SELECT token_hash, protocol_version, capabilities, registered_at, last_seen_at, lease_expires_at FROM workers WHERE id = $1`,
		workerID,
	).Scan(
		&tokenHash,
		&protocol,
		&capabilities,
		&storedRegister,
		&lastSeenAt,
		&leaseExpiresAt,
	)
	if err != nil {
		t.Fatalf("read replaced worker: %v", err)
	}

	if tokenHash != replacement.TokenHash {
		t.Fatalf("replacement token hash = %q, want %q", tokenHash, replacement.TokenHash)
	}
	if protocol != replacement.ProtocolVersion {
		t.Fatalf("replacement protocol = %q, want %q", protocol, replacement.ProtocolVersion)
	}
	if !storedRegister.Equal(replacement.RegisteredAt) {
		t.Fatalf("replacement registered_at = %v, want %v", storedRegister, replacement.RegisteredAt)
	}
	if lastSeenAt.Valid {
		t.Fatalf("last_seen_at after re-registration = %v, want NULL", lastSeenAt.Time)
	}
	if !leaseExpiresAt.Equal(replacement.LeaseExpiresAt) {
		t.Fatalf("replacement lease expiry = %v, want %v", leaseExpiresAt, replacement.LeaseExpiresAt)
	}

	gotCapabilities = nil
	if err := json.Unmarshal(capabilities, &gotCapabilities); err != nil {
		t.Fatalf("decode replacement capabilities: %v", err)
	}
	if len(gotCapabilities) != 2 ||
		gotCapabilities[0] != "webhook" ||
		gotCapabilities[1] != "email" {
		t.Fatalf("replacement capabilities = %v, want [webhook email]", gotCapabilities)
	}

	var rowCount int
	if err := db.QueryRow(
		t.Context(),
		"SELECT COUNT(*) FROM workers WHERE id = $1",
		workerID,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count worker rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("worker rows = %d, want 1", rowCount)
	}
}
