package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"

	"forge/internal/settings"
	"forge/internal/token"
)

type stubSettings struct {
	value  string
	exists bool

	getErr    error
	createErr error
	updateErr error

	createCalls int
	updateCalls int
}

func (s *stubSettings) GetSettingValue(
	_ context.Context,
	_ string,
) (string, error) {
	if s.getErr != nil {
		return "", s.getErr
	}

	if !s.exists {
		return "", settings.ErrSettingNotFound
	}

	return s.value, nil
}

func (s *stubSettings) CreateSetting(
	_ context.Context,
	_, value string,
) error {
	s.createCalls++
	if s.createErr != nil {
		return s.createErr
	}

	s.value = value
	s.exists = true
	return nil
}

func (s *stubSettings) UpdateSettingValue(
	_ context.Context,
	_, value string,
) error {
	s.updateCalls++
	if s.updateErr != nil {
		return s.updateErr
	}

	s.value = value
	s.exists = true
	return nil
}

func newTestService(store *stubSettings) *Service {
	return NewService(store)
}

func TestInitCreatesToken(t *testing.T) {
	store := &stubSettings{}
	service := newTestService(store)

	result, err := service.Execute(context.Background(), CommandInit)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if result.Token == "" {
		t.Fatal("expected generated token")
	}

	if result.AlreadyInitialized {
		t.Fatal("unexpected AlreadyInitialized")
	}

	if !store.exists {
		t.Fatal("token hash was not stored")
	}

	if got, want := store.value, token.HashToken(result.Token); got != want {
		t.Fatal("stored value is not the generated token hash")
	}

	if store.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", store.createCalls)
	}

	if store.updateCalls != 0 {
		t.Fatalf("update calls = %d, want 0", store.updateCalls)
	}
}

func TestInitDoesNotReplaceExistingToken(t *testing.T) {
	store := &stubSettings{
		value:  "existing-hash",
		exists: true,
	}
	service := newTestService(store)

	result, err := service.Execute(context.Background(), CommandInit)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !result.AlreadyInitialized {
		t.Fatal("expected AlreadyInitialized")
	}

	if result.Token != "" {
		t.Fatal("token should not be returned when already initialized")
	}

	if store.value != "existing-hash" {
		t.Fatal("existing hash was changed")
	}

	if store.createCalls != 0 || store.updateCalls != 0 {
		t.Fatal("storage was modified")
	}
}

func TestRotateReplacesToken(t *testing.T) {
	store := &stubSettings{
		value:  "old-hash",
		exists: true,
	}
	service := newTestService(store)

	result, err := service.Execute(context.Background(), CommandRotate)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if result.Token == "" {
		t.Fatal("expected generated token")
	}

	if got, want := store.value, token.HashToken(result.Token); got != want {
		t.Fatal("stored value is not the generated token hash")
	}

	if store.value == "old-hash" {
		t.Fatal("old token hash was not replaced")
	}

	if store.updateCalls != 1 {
		t.Fatalf("update calls = %d, want 1", store.updateCalls)
	}

	if store.createCalls != 0 {
		t.Fatalf("create calls = %d, want 0", store.createCalls)
	}
}

func TestRotateRequiresInitialization(t *testing.T) {
	store := &stubSettings{}
	service := newTestService(store)

	_, err := service.Execute(context.Background(), CommandRotate)

	if !errors.Is(err, ErrTokenNotInitialized) {
		t.Fatalf("Execute() error = %v, want ErrTokenNotInitialized", err)
	}

	if store.updateCalls != 0 {
		t.Fatal("storage was updated")
	}
}

func TestInitReturnsReadError(t *testing.T) {
	store := &stubSettings{
		getErr: errors.New("storage unavailable"),
	}
	service := newTestService(store)

	_, err := service.Execute(context.Background(), CommandInit)

	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "check current token") {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.createCalls != 0 {
		t.Fatal("storage was modified after read failure")
	}
}

func TestRotateReturnsReadError(t *testing.T) {
	store := &stubSettings{
		getErr: errors.New("storage unavailable"),
	}
	service := newTestService(store)

	_, err := service.Execute(context.Background(), CommandRotate)

	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "check current token") {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.updateCalls != 0 {
		t.Fatal("storage was updated after read failure")
	}
}

func TestInitReturnsCreateError(t *testing.T) {
	store := &stubSettings{
		createErr: errors.New("storage unavailable"),
	}
	service := newTestService(store)

	_, err := service.Execute(context.Background(), CommandInit)

	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "save bootstrap token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRotateReturnsUpdateError(t *testing.T) {
	store := &stubSettings{
		value:     "old-hash",
		exists:    true,
		updateErr: errors.New("storage unavailable"),
	}
	service := newTestService(store)

	_, err := service.Execute(context.Background(), CommandRotate)

	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "update bootstrap token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteRejectsUnknownCommand(t *testing.T) {
	service := newTestService(&stubSettings{})

	_, err := service.Execute(context.Background(), Command("delete"))

	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}
