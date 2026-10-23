package postgres

import (
	"errors"
	"testing"

	"forge/internal/settings"
)

func TestSettingRepository_CreateAndGet(t *testing.T) {
	db, err := getTestDb(t, true)
	if err != nil {
		t.Fatalf("test db connection failed: %v", err)
	}

	repo := NewSettingRepository(db)

	err = repo.Create(t.Context(), "test_key", "test_value")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.Get(t.Context(), "test_key")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "test_value" {
		t.Errorf("Get() = %q, want %q", got, "test_value")
	}
}

func TestSettingRepository_GetNotFound(t *testing.T) {
	db, err := getTestDb(t, true)
	if err != nil {
		t.Fatalf("test db connection failed: %v", err)
	}

	repo := NewSettingRepository(db)

	got, err := repo.Get(t.Context(), "missing_key")
	if !errors.Is(err, settings.ErrSettingNotFound) {
		t.Fatalf("Get() error = %v, want %v", err, settings.ErrSettingNotFound)
	}
	if got != "" {
		t.Errorf("Get() = %q, want empty string", got)
	}
}

func TestSettingRepository_CreateDuplicateKey(t *testing.T) {
	db, err := getTestDb(t, true)
	if err != nil {
		t.Fatalf("test db connection failed: %v", err)
	}

	repo := NewSettingRepository(db)

	if err := repo.Create(t.Context(), "duplicate_key", "first"); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	err = repo.Create(t.Context(), "duplicate_key", "second")
	if err == nil {
		t.Fatal("second Create() error = nil, want duplicate-key error")
	}

	got, err := repo.Get(t.Context(), "duplicate_key")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "first" {
		t.Errorf("stored value = %q, want %q", got, "first")
	}
}

func TestSettingRepository_Update(t *testing.T) {
	db, err := getTestDb(t, true)
	if err != nil {
		t.Fatalf("test db connection failed: %v", err)
	}

	repo := NewSettingRepository(db)

	if err := repo.Create(t.Context(), "update_key", "old_value"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := repo.Update(t.Context(), "update_key", "new_value"); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := repo.Get(t.Context(), "update_key")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "new_value" {
		t.Errorf("Get() = %q, want %q", got, "new_value")
	}
}

func TestSettingRepository_UpdateNotFound(t *testing.T) {
	db, err := getTestDb(t, true)
	if err != nil {
		t.Fatalf("test db connection failed: %v", err)
	}

	repo := NewSettingRepository(db)

	err = repo.Update(t.Context(), "missing_key", "new_value")
	if !errors.Is(err, settings.ErrSettingNotFound) {
		t.Fatalf("Update() error = %v, want %v", err, settings.ErrSettingNotFound)
	}
}
