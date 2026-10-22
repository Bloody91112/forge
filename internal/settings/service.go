package settings

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Repository interface {
	Get(ctx context.Context, key string) (string, error)
	Create(ctx context.Context, key string, value string) error
	Update(ctx context.Context, key string, value string) error
}

var (
	ErrSettingNotFound = errors.New("setting not found")
	ErrEmptySettingKey = errors.New("empty setting key provided")
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetSettingValue(ctx context.Context, key string) (string, error) {
	err := validateKey(key)
	if err != nil {
		return "", err
	}

	setting, err := s.repo.Get(ctx, key)

	if err != nil {
		return "", fmt.Errorf("get setting: %w", err)
	}

	return setting, nil
}

func (s *Service) UpdateSettingValue(ctx context.Context, key string, value string) error {
	err := validateKey(key)
	if err != nil {
		return err
	}

	if err := s.repo.Update(ctx, key, value); err != nil {
		return fmt.Errorf("update setting: %w", err)
	}

	return nil
}

func (s *Service) CreateSetting(ctx context.Context, key string, value string) error {
	err := validateKey(key)
	if err != nil {
		return err
	}

	if err := s.repo.Create(ctx, key, value); err != nil {
		return fmt.Errorf("create setting: %w", err)
	}

	return nil
}

func validateKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return ErrEmptySettingKey
	}

	return nil
}
