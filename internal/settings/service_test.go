package settings

import (
	"context"
	"errors"
	"testing"
)

type fakeRepository struct {
	getValue  string
	getErr    error
	createErr error
	updateErr error

	getCalls    int
	createCalls int
	updateCalls int

	gotKey   string
	gotValue string
}

func (r *fakeRepository) Get(_ context.Context, key string) (string, error) {
	r.getCalls++
	r.gotKey = key
	return r.getValue, r.getErr
}

func (r *fakeRepository) Create(_ context.Context, key, value string) error {
	r.createCalls++
	r.gotKey = key
	r.gotValue = value
	return r.createErr
}

func (r *fakeRepository) Update(_ context.Context, key, value string) error {
	r.updateCalls++
	r.gotKey = key
	r.gotValue = value
	return r.updateErr
}

func TestService_GetSettingValue(t *testing.T) {
	t.Run("returns setting value", func(t *testing.T) {
		repo := &fakeRepository{getValue: "secret-hash"}
		service := NewService(repo)

		got, err := service.GetSettingValue(t.Context(), "worker_bootstrap_token_hash")
		if err != nil {
			t.Fatalf("GetSettingValue() error = %v", err)
		}
		if got != "secret-hash" {
			t.Errorf("GetSettingValue() = %q, want %q", got, "secret-hash")
		}
		if repo.gotKey != "worker_bootstrap_token_hash" {
			t.Errorf("repository key = %q, want %q", repo.gotKey, "worker_bootstrap_token_hash")
		}
	})

	t.Run("rejects empty key", func(t *testing.T) {
		for _, key := range []string{"", " ", "\t\n"} {
			t.Run(key, func(t *testing.T) {
				repo := &fakeRepository{}
				service := NewService(repo)

				_, err := service.GetSettingValue(t.Context(), key)
				if !errors.Is(err, ErrEmptySettingKey) {
					t.Fatalf("error = %v, want %v", err, ErrEmptySettingKey)
				}
				if repo.getCalls != 0 {
					t.Error("repository must not be called for an invalid key")
				}
			})
		}
	})

	t.Run("wraps repository error", func(t *testing.T) {
		repoErr := errors.New("database unavailable")
		service := NewService(&fakeRepository{getErr: repoErr})

		_, err := service.GetSettingValue(t.Context(), "some_key")
		if !errors.Is(err, repoErr) {
			t.Fatalf("error = %v, want wrapped error %v", err, repoErr)
		}
	})
}

func TestService_CreateSetting(t *testing.T) {
	t.Run("creates setting", func(t *testing.T) {
		repo := &fakeRepository{}
		service := NewService(repo)

		err := service.CreateSetting(t.Context(), "some_key", "some_value")
		if err != nil {
			t.Fatalf("CreateSetting() error = %v", err)
		}
		if repo.createCalls != 1 {
			t.Errorf("Create calls = %d, want 1", repo.createCalls)
		}
		if repo.gotKey != "some_key" || repo.gotValue != "some_value" {
			t.Errorf("repository received (%q, %q), want (%q, %q)",
				repo.gotKey, repo.gotValue, "some_key", "some_value")
		}
	})

	t.Run("rejects empty key", func(t *testing.T) {
		repo := &fakeRepository{}
		service := NewService(repo)

		err := service.CreateSetting(t.Context(), "  ", "some_value")
		if !errors.Is(err, ErrEmptySettingKey) {
			t.Fatalf("error = %v, want %v", err, ErrEmptySettingKey)
		}
		if repo.createCalls != 0 {
			t.Error("repository must not be called for an invalid key")
		}
	})

	t.Run("wraps repository error", func(t *testing.T) {
		repoErr := errors.New("insert failed")
		service := NewService(&fakeRepository{createErr: repoErr})

		err := service.CreateSetting(t.Context(), "some_key", "some_value")
		if !errors.Is(err, repoErr) {
			t.Fatalf("error = %v, want wrapped error %v", err, repoErr)
		}
	})
}

func TestService_UpdateSettingValue(t *testing.T) {
	t.Run("updates setting", func(t *testing.T) {
		repo := &fakeRepository{}
		service := NewService(repo)

		err := service.UpdateSettingValue(t.Context(), "some_key", "new_value")
		if err != nil {
			t.Fatalf("UpdateSettingValue() error = %v", err)
		}
		if repo.updateCalls != 1 {
			t.Errorf("Update calls = %d, want 1", repo.updateCalls)
		}
		if repo.gotKey != "some_key" || repo.gotValue != "new_value" {
			t.Errorf("repository received (%q, %q), want (%q, %q)",
				repo.gotKey, repo.gotValue, "some_key", "new_value")
		}
	})

	t.Run("rejects empty key", func(t *testing.T) {
		repo := &fakeRepository{}
		service := NewService(repo)

		err := service.UpdateSettingValue(t.Context(), "\n ", "new_value")
		if !errors.Is(err, ErrEmptySettingKey) {
			t.Fatalf("error = %v, want %v", err, ErrEmptySettingKey)
		}
		if repo.updateCalls != 0 {
			t.Error("repository must not be called for an invalid key")
		}
	})

	t.Run("wraps repository error", func(t *testing.T) {
		repoErr := errors.New("update failed")
		service := NewService(&fakeRepository{updateErr: repoErr})

		err := service.UpdateSettingValue(t.Context(), "some_key", "new_value")
		if !errors.Is(err, repoErr) {
			t.Fatalf("error = %v, want wrapped error %v", err, repoErr)
		}
	})
}
