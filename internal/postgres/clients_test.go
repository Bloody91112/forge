package postgres

import (
	"crypto/rand"
	"errors"
	"forge/internal/auth"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
)

func TestClientRepository_GetClientIDByTokenHash_ReturnsExistingClient(t *testing.T) {
	db, err := getTestDb(t, true)

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
	db, err := getTestDb(t, true)

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
	db, err := getTestDb(t, true)

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
