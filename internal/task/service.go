package task

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var (
	ErrInvalidInput        = errors.New("invalid input")
	ErrMissingClientID     = errors.New("missing client id in context")
	ErrIdempotencyConflict = errors.New("idempotency key conflict")
)

const DefaultTaskMaxAttempts = 5
const DefaultTaskTimeout = 300

type contextKey string

const clientIDKey contextKey = "client_id"

type Repository interface {
	CreateOrGetByIdempotencyKey(ctx context.Context, task Task) (Task, bool, error)
	GetByID(ctx context.Context, taskID uuid.UUID, clientID uuid.UUID) (Task, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func WithClientID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, clientIDKey, id)
}

func ClientIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(clientIDKey).(uuid.UUID)
	if !ok {
		return uuid.Nil, false
	}
	return id, true
}

func (s *Service) Create(ctx context.Context, dto CreateTaskInput) (Task, error) {
	var maxAttempts int64 = DefaultTaskMaxAttempts
	if dto.MaxAttempts != nil {
		maxAttempts = *dto.MaxAttempts
	}

	var timeoutSeconds int64 = DefaultTaskTimeout
	if dto.TimeoutSeconds != nil {
		timeoutSeconds = *dto.TimeoutSeconds
	}

	if maxAttempts <= 0 {
		return Task{}, fmt.Errorf("max_attempts must be greater than zero, got %d: %w", maxAttempts, ErrInvalidInput)
	}

	if timeoutSeconds <= 0 {
		return Task{}, fmt.Errorf("timeout_seconds must be greater than zero, got %d: %w", timeoutSeconds, ErrInvalidInput)
	}

	if dto.Type == "" {
		return Task{}, fmt.Errorf("task type required, got empty string: %w", ErrInvalidInput)
	}

	if dto.IdempotencyKey == "" {
		return Task{}, fmt.Errorf("idempotency key required, got empty string: %w", ErrInvalidInput)
	}

	if !json.Valid(dto.Payload) {
		return Task{}, fmt.Errorf("payload must be valid JSON: %w", ErrInvalidInput)
	}

	hashStr, err := calculateRequestHash(dto.Payload, dto.Type, timeoutSeconds, maxAttempts)
	if err != nil {
		return Task{}, fmt.Errorf("calculate request hash: %w", err)
	}

	clientID, ok := ClientIDFromContext(ctx)
	if !ok {
		return Task{}, ErrMissingClientID
	}

	task := Task{
		ID:             uuid.New(),
		ClientID:       clientID,
		IdempotencyKey: dto.IdempotencyKey,
		RequestHash:    hashStr,
		Type:           dto.Type,
		Payload:        dto.Payload,
		Status:         StatusQueued,
		MaxAttempts:    maxAttempts,
		TimeoutSeconds: timeoutSeconds,
	}

	createdTask, created, err := s.repo.CreateOrGetByIdempotencyKey(ctx, task)

	if err != nil {
		return Task{}, fmt.Errorf("create task in repository: %w", err)
	}

	if !created && createdTask.RequestHash != hashStr {
		return createdTask, ErrIdempotencyConflict
	}

	return createdTask, nil
}

func (s *Service) GetByID(ctx context.Context, taskID uuid.UUID) (Task, error) {
	clientID, ok := ClientIDFromContext(ctx)

	if !ok {
		return Task{}, ErrMissingClientID
	}

	task, err := s.repo.GetByID(ctx, taskID, clientID)

	if err != nil {
		return Task{}, fmt.Errorf("get task: %w", err)
	}

	return task, nil
}

func calculateRequestHash(
	payload json.RawMessage,
	taskType string,
	timeoutSeconds int64,
	maxAttempts int64,
) (string, error) {
	var decoded any

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()

	if err := decoder.Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode payload: %w", err)
	}

	canonicalPayload, err := json.Marshal(decoded)
	if err != nil {
		return "", fmt.Errorf("marshal canonical payload: %w", err)
	}

	hashData := struct {
		Payload        json.RawMessage `json:"payload"`
		Type           string          `json:"type"`
		TimeoutSeconds int64           `json:"timeout_seconds"`
		MaxAttempts    int64           `json:"max_attempts"`
	}{
		Payload:        canonicalPayload,
		Type:           taskType,
		TimeoutSeconds: timeoutSeconds,
		MaxAttempts:    maxAttempts,
	}

	data, err := json.Marshal(hashData)
	if err != nil {
		return "", fmt.Errorf("marshal hash data: %w", err)
	}

	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
