package postgres

import (
	"crypto/rand"
	"errors"
	"forge/internal/auth"
	"forge/internal/testutil"
	"forge/internal/token"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
)

func TestClientRepository_GetClientIDByTokenHash_ReturnsExistingClient(t *testing.T) {
	db, err := testutil.OpenPostgres(t, true)

	if err != nil {
		t.Fatalf("test db connection failed: %v", err)
	}

	repo := NewClientRepository(db)

	client, err := repo.CreateClient(t.Context(), auth.Client{
		ID:        uuid.New(),
		Name:      "BOB",
		TokenHash: rand.Text(),
	})

	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	gotClient, err := repo.GetClientByTokenHash(t.Context(), client.TokenHash)

	if err != nil {
		t.Fatalf("failed to get client: %v", err)
	}

	if gotClient.ID != client.ID {
		t.Fatalf("expected id: %s, got: %s", client.ID, gotClient.ID)
	}
}

func TestClientRepository_GetClientIDByTokenHash_ReturnsErrorOnClientMiss(t *testing.T) {
	db, err := testutil.OpenPostgres(t, true)

	if err != nil {
		t.Fatalf("test db connection failed: %v", err)
	}

	repo := NewClientRepository(db)

	gotClient, err := repo.GetClientByTokenHash(t.Context(), "no-client")

	if !errors.Is(err, auth.ErrClientNotFound) {
		t.Fatalf("expected err: %v, got: %v", auth.ErrClientNotFound, err)
	}

	if gotClient.ID != uuid.Nil {
		t.Fatalf("expected id: %s, got: %s", uuid.Nil, gotClient.ID)
	}
}

func TestClientRepository_CreateClient(t *testing.T) {
	db, err := testutil.OpenPostgres(t, true)

	if err != nil {
		t.Fatalf("test db connection failed: %v", err)
	}

	repo := NewClientRepository(db)

	expected := auth.Client{
		ID:        uuid.New(),
		Name:      "BOB",
		TokenHash: rand.Text(),
	}

	created, err := repo.CreateClient(t.Context(), expected)

	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if diff := cmp.Diff(expected, created, cmpopts.IgnoreFields(auth.Client{}, "CreatedAt")); diff != "" {
		t.Errorf("client mismatch (-want +got):\n%s", diff)
	}

	gotClient, err := repo.GetClientByTokenHash(t.Context(), expected.TokenHash)

	if err != nil {
		t.Fatalf("failed to get client: %v", err)
	}

	if expected.ID != gotClient.ID {
		t.Fatalf("expected client id: %s, got: %s", expected.ID, gotClient.ID)
	}

}

func TestClientRepository_RotateClientToken(t *testing.T) {
	db, err := testutil.OpenPostgres(t, true)
	if err != nil {
		t.Fatalf("test db connection failed: %v", err)
	}

	repo := NewClientRepository(db)

	oldTokenHash := token.HashToken(rand.Text())
	newTokenHash := token.HashToken(rand.Text())

	client, err := repo.CreateClient(t.Context(), auth.Client{
		ID:        uuid.New(),
		Name:      "test-client",
		TokenHash: oldTokenHash,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	rotationStartedAt := time.Now()

	rotatedClient, err := repo.RotateClientToken(
		t.Context(),
		client.ID,
		newTokenHash,
	)
	if err != nil {
		t.Fatalf("failed to rotate client token: %v", err)
	}

	rotationFinishedAt := time.Now()

	if rotatedClient.TokenHash != newTokenHash {
		t.Errorf(
			"current token hash = %q, want %q",
			rotatedClient.TokenHash,
			newTokenHash,
		)
	}

	if rotatedClient.PreviousTokenHash == nil {
		t.Fatal("previous token hash is nil")
	}

	if *rotatedClient.PreviousTokenHash != oldTokenHash {
		t.Errorf(
			"previous token hash = %q, want %q",
			*rotatedClient.PreviousTokenHash,
			oldTokenHash,
		)
	}

	if rotatedClient.PreviousTokenExpiresAt == nil {
		t.Fatal("previous token expiration is nil")
	}

	expiresAt := *rotatedClient.PreviousTokenExpiresAt

	if expiresAt.Before(rotationStartedAt.Add(5*time.Minute)) ||
		expiresAt.After(rotationFinishedAt.Add(5*time.Minute)) {
		t.Errorf("unexpected previous token expiration: %s", expiresAt)
	}

	// Previous token must remain valid during the overlap period.
	gotClient, err := repo.GetClientByTokenHash(t.Context(), oldTokenHash)
	if err != nil {
		t.Fatalf("previous token should be valid: %v", err)
	}

	if gotClient.ID != client.ID {
		t.Errorf("client ID = %s, want %s", gotClient.ID, client.ID)
	}

	// New token must be valid too.
	gotClient, err = repo.GetClientByTokenHash(t.Context(), newTokenHash)
	if err != nil {
		t.Fatalf("new token should be valid: %v", err)
	}

	if gotClient.ID != client.ID {
		t.Errorf("client ID = %s, want %s", gotClient.ID, client.ID)
	}

	// Expire the previous token without waiting five minutes.
	_, err = db.Exec(
		t.Context(),
		`
			UPDATE clients
			SET previous_token_expires_at = clock_timestamp() - INTERVAL '1 second'
			WHERE id = $1
		`,
		client.ID,
	)
	if err != nil {
		t.Fatalf("failed to expire previous token: %v", err)
	}

	_, err = repo.GetClientByTokenHash(t.Context(), oldTokenHash)
	if !errors.Is(err, auth.ErrClientNotFound) {
		t.Fatalf(
			"expired previous token error = %v, want %v",
			err,
			auth.ErrClientNotFound,
		)
	}

	// Current token must remain valid after previous token expiration.
	gotClient, err = repo.GetClientByTokenHash(t.Context(), newTokenHash)
	if err != nil {
		t.Fatalf("current token should remain valid: %v", err)
	}

	if gotClient.ID != client.ID {
		t.Errorf("client ID = %s, want %s", gotClient.ID, client.ID)
	}
}
