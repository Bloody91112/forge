package task

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"forge/internal/worker"
	"slices"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidInput            = errors.New("invalid input")
	ErrMissingClientID         = errors.New("missing client id in context")
	ErrIdempotencyConflict     = errors.New("idempotency key conflict")
	ErrTaskTypeNotSupported    = errors.New("task not supported")
	ErrNoActiveWorkers         = errors.New("no active workers found")
	ErrNoAvailableTasks        = errors.New("no available tasks found")
	ErrWorkerNotFound          = errors.New("worker not found, not active or has no capabilities")
	ErrInvalidRequestStructure = errors.New("invalid request structure")
	ErrPayloadTooLarge         = errors.New("payload too large")
	ErrTaskLeaseLost           = errors.New("task lease lost")
	ErrTaskAttemptNotFound     = errors.New("task attempt not found")
)

const (
	DefaultTaskMaxAttempts = 5
	DefaultTaskTimeout     = 300
	MaxTaskAttempts        = 10
	MaxTaskTimeout         = 3600
	MaxPayloadSize         = 1 << 20 // 1 MiB
	LeaseSeconds           = 30
)

type CreateResult struct {
	Task    Task
	Created bool
}

type contextKey string

const clientIDKey contextKey = "client_id"

type Repository interface {
	CreateOrGetByIdempotencyKey(ctx context.Context, task Task) (Task, bool, error)
	GetByID(ctx context.Context, taskID uuid.UUID, clientID uuid.UUID) (Task, error)
	GetByIdempotencyKey(ctx context.Context, clientID uuid.UUID, key string) (Task, error)
	FindAndClaimForWorker(ctx context.Context, workerID uuid.UUID, workerCapabilities []string, leaseSeconds int) (Task, error)
	RenewTaskLeaseForWorker(ctx context.Context, taskID, workerID, leaseToken uuid.UUID, leaseSeconds int) (time.Time, error)
	MarkTaskAsCompleted(ctx context.Context, taskID, workerID, leaseToken uuid.UUID, result json.RawMessage) error
	MarkTaskAsFailed(ctx context.Context, taskID, workerID, leaseToken uuid.UUID, errCode, errMessage string, retryable bool) error
}

type WorkerRepository interface {
	GetActiveWorkersCapabilities(ctx context.Context) ([][]string, error)
	GetActiveWorkerCapabilities(ctx context.Context, id uuid.UUID) ([]string, error)
}

type Service struct {
	repo        Repository
	workersRepo WorkerRepository
}

func NewService(repo Repository, workersRepo WorkerRepository) *Service {
	return &Service{repo: repo, workersRepo: workersRepo}
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

func (s *Service) Create(ctx context.Context, dto CreateTaskInput) (CreateResult, error) {
	var maxAttempts int64 = DefaultTaskMaxAttempts
	if dto.MaxAttempts != nil {
		maxAttempts = *dto.MaxAttempts
	}

	var timeoutSeconds int64 = DefaultTaskTimeout
	if dto.TimeoutSeconds != nil {
		timeoutSeconds = *dto.TimeoutSeconds
	}

	if maxAttempts < 1 || maxAttempts > MaxTaskAttempts {
		return CreateResult{}, fmt.Errorf("max_attempts must be between 1 and %d, got %d: %w", MaxTaskAttempts, maxAttempts, ErrInvalidInput)
	}

	if timeoutSeconds < 1 || timeoutSeconds > MaxTaskTimeout {
		return CreateResult{}, fmt.Errorf("timeout_seconds must be between 1 and %d, got %d: %w", MaxTaskTimeout, timeoutSeconds, ErrInvalidInput)
	}

	if dto.Type == "" {
		return CreateResult{}, fmt.Errorf("task type required, got empty string: %w", ErrInvalidInput)
	}

	if dto.IdempotencyKey == "" {
		return CreateResult{}, fmt.Errorf("idempotency key required, got empty string: %w", ErrInvalidInput)
	}

	payload := bytes.TrimSpace(dto.Payload)

	if len(payload) == 0 {
		return CreateResult{}, fmt.Errorf("payload must be a JSON object: %w", ErrInvalidRequestStructure)
	}

	if len(payload) > MaxPayloadSize {
		return CreateResult{}, fmt.Errorf("payload exceeds %d bytes: %w", MaxPayloadSize, ErrPayloadTooLarge)
	}

	if !json.Valid(payload) || payload[0] != '{' {
		return CreateResult{}, fmt.Errorf("payload must be a JSON object: %w", ErrInvalidRequestStructure)
	}

	hashStr, err := calculateRequestHash(dto.Payload, dto.Type, timeoutSeconds, maxAttempts)
	if err != nil {
		return CreateResult{}, fmt.Errorf("calculate request hash: %w", err)
	}

	clientID, ok := ClientIDFromContext(ctx)
	if !ok {
		return CreateResult{}, ErrMissingClientID
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

	existingTask, err := s.repo.GetByIdempotencyKey(ctx, clientID, dto.IdempotencyKey)

	if err == nil {
		if existingTask.RequestHash != hashStr {
			return CreateResult{Task: existingTask}, ErrIdempotencyConflict
		}

		return CreateResult{Task: existingTask, Created: false}, nil
	}

	if !errors.Is(err, ErrTaskNotFound) {
		return CreateResult{}, fmt.Errorf("get task by idempotency key: %w", err)
	}

	workersCapabilities, err := s.workersRepo.GetActiveWorkersCapabilities(ctx)
	if err != nil {
		return CreateResult{}, fmt.Errorf("get active workers capabilities: %w", err)
	}

	if len(workersCapabilities) == 0 {
		return CreateResult{}, ErrNoActiveWorkers
	}

	for _, workerCapabilities := range workersCapabilities {
		if !slices.Contains(workerCapabilities, task.Type) {
			return CreateResult{}, ErrTaskTypeNotSupported
		}
	}

	createdTask, created, err := s.repo.CreateOrGetByIdempotencyKey(ctx, task)

	if err != nil {
		return CreateResult{}, fmt.Errorf("create task in repository: %w", err)
	}

	if !created && createdTask.RequestHash != hashStr {
		return CreateResult{Task: createdTask}, ErrIdempotencyConflict
	}

	return CreateResult{
		Task:    createdTask,
		Created: created,
	}, nil
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

func (s *Service) Claim(ctx context.Context, dto ClaimTaskInput) (Task, error) {
	if err := checkWorkerID(ctx, dto.WorkerID); err != nil {
		return Task{}, err
	}

	workerCapabilities, err := s.workersRepo.GetActiveWorkerCapabilities(ctx, dto.WorkerID)

	if err != nil {
		return Task{}, fmt.Errorf("get worker capabilities: %w", err)
	}

	claimedTask, err := s.repo.FindAndClaimForWorker(ctx, dto.WorkerID, workerCapabilities, LeaseSeconds)

	if err != nil {
		return Task{}, fmt.Errorf("claim task: %w", err)
	}

	return claimedTask, nil
}

func (s *Service) Renew(ctx context.Context, dto RenewTaskInput) (time.Time, error) {
	if err := checkWorkerID(ctx, dto.WorkerID); err != nil {
		return time.Time{}, err
	}

	if err := checkNullUuids(dto.TaskID, dto.WorkerID, dto.LeaseToken); err != nil {
		return time.Time{}, err
	}

	leaseExpiresAt, err := s.repo.RenewTaskLeaseForWorker(ctx, dto.TaskID, dto.WorkerID, dto.LeaseToken, LeaseSeconds)

	if err != nil {
		return time.Time{}, fmt.Errorf("renew task lease: %w", err)
	}

	return leaseExpiresAt, nil
}

func (s *Service) Complete(ctx context.Context, dto CompleteTaskInput) error {
	if err := checkWorkerID(ctx, dto.WorkerID); err != nil {
		return err
	}

	payload := bytes.TrimSpace(dto.Result)

	if len(payload) == 0 {
		return fmt.Errorf("payload must be a JSON object: %w", ErrInvalidRequestStructure)
	}

	if len(payload) > MaxPayloadSize {
		return fmt.Errorf("payload exceeds %d bytes: %w", MaxPayloadSize, ErrPayloadTooLarge)
	}

	if !json.Valid(payload) || payload[0] != '{' {
		return fmt.Errorf("payload must be a JSON object: %w", ErrInvalidRequestStructure)
	}

	if err := checkNullUuids(dto.TaskID, dto.WorkerID, dto.LeaseToken); err != nil {
		return err
	}

	if err := s.repo.MarkTaskAsCompleted(ctx, dto.TaskID, dto.WorkerID, dto.LeaseToken, dto.Result); err != nil {
		return fmt.Errorf("mark task completed: %w", err)
	}

	return nil
}

func (s *Service) Fail(ctx context.Context, dto FailTaskInput) error {
	if err := checkWorkerID(ctx, dto.WorkerID); err != nil {
		return err
	}

	if dto.Retryable == nil {
		return fmt.Errorf("retryable field not set: %w", ErrInvalidInput)
	}

	if dto.Error.Code == "" {
		return fmt.Errorf("no error code: %w", ErrInvalidInput)
	}

	if dto.Error.Message == "" {
		return fmt.Errorf("no error message: %w", ErrInvalidInput)
	}

	if err := checkNullUuids(dto.TaskID, dto.WorkerID, dto.LeaseToken); err != nil {
		return err
	}

	if err := s.repo.MarkTaskAsFailed(ctx, dto.TaskID, dto.WorkerID, dto.LeaseToken, dto.Error.Code, dto.Error.Message, *dto.Retryable); err != nil {
		return fmt.Errorf("mark task failed: %w", err)
	}

	return nil
}

func calculateRequestHash(payload json.RawMessage, taskType string, timeoutSeconds int64, maxAttempts int64) (string, error) {
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

func checkWorkerID(ctx context.Context, id uuid.UUID) error {
	if err := checkNullUuids(id); err != nil {
		return err
	}

	contextWorkerID, ok := worker.IDFromContext(ctx)

	if !ok {
		return worker.ErrMissingWorkerID
	}

	if id != contextWorkerID {
		return fmt.Errorf("worker id conflict, context id: %s, provided id: %s: %w", contextWorkerID, id, ErrInvalidInput)
	}

	return nil
}

func checkNullUuids(args ...uuid.UUID) error {
	for _, uuidValue := range args {
		if uuidValue == uuid.Nil {
			return fmt.Errorf("nil uuid provided: %w", ErrInvalidInput)
		}
	}

	return nil
}
