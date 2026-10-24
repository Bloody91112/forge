package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"forge/internal/settings"
	"forge/internal/token"
)

const settingKey = "worker_bootstrap_token_hash"

var ErrTokenNotInitialized = errors.New("bootstrap token not initialized; run 'forge bootstrap init' first")

type Settings interface {
	GetSettingValue(ctx context.Context, key string) (string, error)
	CreateSetting(ctx context.Context, key, value string) error
	UpdateSettingValue(ctx context.Context, key, value string) error
}

type Command string

const (
	CommandInit   Command = "init"
	CommandRotate Command = "rotate"
)

type Result struct {
	Token              string
	AlreadyInitialized bool
}

type Service struct {
	settings Settings
}

func NewService(settings Settings) *Service {
	return &Service{settings: settings}
}

func (s *Service) Execute(ctx context.Context, command Command) (Result, error) {
	switch command {
	case CommandInit:
		return s.init(ctx)
	case CommandRotate:
		return s.rotate(ctx)
	default:
		return Result{}, fmt.Errorf("unknown bootstrap command: %s", command)
	}
}

func (s *Service) init(ctx context.Context) (Result, error) {
	_, err := s.settings.GetSettingValue(ctx, settingKey)
	if err == nil {
		return Result{AlreadyInitialized: true}, nil
	}

	if !errors.Is(err, settings.ErrSettingNotFound) {
		return Result{}, fmt.Errorf("check current token: %w", err)
	}

	generatedToken, err := token.GenerateBootstrapToken()
	if err != nil {
		return Result{}, fmt.Errorf("generate token: %w", err)
	}

	if err := s.settings.CreateSetting(
		ctx,
		settingKey,
		token.HashToken(generatedToken),
	); err != nil {
		return Result{}, fmt.Errorf("save bootstrap token: %w", err)
	}

	return Result{Token: generatedToken}, nil
}

func (s *Service) rotate(ctx context.Context) (Result, error) {
	_, err := s.settings.GetSettingValue(ctx, settingKey)
	if errors.Is(err, settings.ErrSettingNotFound) {
		return Result{}, ErrTokenNotInitialized
	}

	if err != nil {
		return Result{}, fmt.Errorf("check current token: %w", err)
	}

	generatedToken, err := token.GenerateBootstrapToken()
	if err != nil {
		return Result{}, fmt.Errorf("generate token: %w", err)
	}

	if err := s.settings.UpdateSettingValue(
		ctx,
		settingKey,
		token.HashToken(generatedToken),
	); err != nil {
		return Result{}, fmt.Errorf("update bootstrap token: %w", err)
	}

	return Result{Token: generatedToken}, nil
}

func (s *Service) Validate(ctx context.Context, inputBootstrapToken string) (bool, error) {
	storageTokenHash, err := s.settings.GetSettingValue(ctx, settingKey)
	if err != nil {
		return false, fmt.Errorf("get token from storage: %w", err)
	}

	return storageTokenHash == token.HashToken(inputBootstrapToken), nil
}
