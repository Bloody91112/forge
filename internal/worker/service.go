package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"forge/internal/bootstrap"
	"forge/internal/token"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidInput               = errors.New("invalid worker input")
	ErrInvalidBootstrapToken      = errors.New("invalid bootstrap token")
	ErrUnsupportedProtocolVersion = errors.New("unsupported protocol version")
	ErrWorkerNotFound             = errors.New("worker not found")
	ErrMissingWorkerID            = errors.New("missing worker id in context")
)

const LeaseSeconds = 30
const RequiredProtocolVersion = "1"

type contextKey string

const workerIDKey contextKey = "worker_id"

type Repository interface {
	Register(ctx context.Context, worker Worker) error
	ExtendLease(ctx context.Context, id uuid.UUID, newLeaseEndTime time.Time, seenAt time.Time) error
}

type Service struct {
	workersRepo      Repository
	bootstrapService *bootstrap.Service
}

func NewService(workersRepo Repository, bootstrapService *bootstrap.Service) *Service {
	return &Service{
		workersRepo:      workersRepo,
		bootstrapService: bootstrapService,
	}
}

func WithID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, workerIDKey, id)
}

func IDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(workerIDKey).(uuid.UUID)
	if !ok {
		return uuid.Nil, false
	}
	return id, true
}

func (s *Service) Register(ctx context.Context, bootstrapToken string, input RegisterRequestDTO) (RegisterResponseDTO, error) {
	if bootstrapToken == "" {
		return RegisterResponseDTO{}, fmt.Errorf("bootstrap token empty string provided: %w", ErrInvalidInput)
	}

	if input.ID == uuid.Nil {
		return RegisterResponseDTO{}, fmt.Errorf("nil uuid provided: %w", ErrInvalidInput)
	}

	if input.ProtocolVersion != RequiredProtocolVersion {
		return RegisterResponseDTO{}, fmt.Errorf("unsupported protocol version %q: %w", input.ProtocolVersion, ErrUnsupportedProtocolVersion)
	}

	capabilities, err := extractWorkerCapabilities(input.Capabilities)
	if err != nil {
		return RegisterResponseDTO{}, err
	}

	tokenOk, err := s.bootstrapService.Validate(ctx, bootstrapToken)
	if err != nil {
		return RegisterResponseDTO{}, fmt.Errorf("failed to validate token, err: %w", err)
	}

	if !tokenOk {
		return RegisterResponseDTO{}, ErrInvalidBootstrapToken
	}

	workerToken, err := token.GenerateWorkerToken()

	if err != nil {
		return RegisterResponseDTO{}, fmt.Errorf("generate worker token: %w", err)
	}

	now := time.Now()

	worker := Worker{
		ID:              input.ID,
		TokenHash:       token.HashToken(workerToken),
		ProtocolVersion: input.ProtocolVersion,
		Capabilities:    capabilities,
		RegisteredAt:    now,
		LeaseExpiresAt:  now.Add(LeaseSeconds * time.Second),
	}

	if err := s.workersRepo.Register(ctx, worker); err != nil {
		return RegisterResponseDTO{}, fmt.Errorf("failed to register worker: %w", err)
	}

	output := RegisterResponseDTO{
		ID:             worker.ID,
		Token:          workerToken,
		LeaseExpiresAt: worker.LeaseExpiresAt,
	}

	return output, nil
}

func extractWorkerCapabilities(inputCapabilities json.RawMessage) ([]string, error) {
	var capabilities []string

	if err := json.Unmarshal(inputCapabilities, &capabilities); err != nil {
		return nil, fmt.Errorf("capabilities must be an array of strings: %w", ErrInvalidInput)
	}

	if capabilities == nil {
		return nil, fmt.Errorf("capabilities must be an array, not null: %w", ErrInvalidInput)
	}

	seen := make(map[string]struct{}, len(capabilities))

	for _, capability := range capabilities {
		if strings.TrimSpace(capability) == "" {
			return nil, fmt.Errorf("capability must not be empty: %w", ErrInvalidInput)
		}

		if capability != strings.TrimSpace(capability) {
			return nil, fmt.Errorf("capability must not contain leading or trailing whitespace: %q: %w",
				capability, ErrInvalidInput)
		}

		if _, exists := seen[capability]; exists {
			return nil, fmt.Errorf("duplicate capability %q: %w", capability, ErrInvalidInput)
		}

		seen[capability] = struct{}{}
	}

	return capabilities, nil
}

func (s *Service) ExtendLease(ctx context.Context, input HeartbeatRequestDTO) (HeartbeatResponseDTO, error) {
	if input.WorkerID == uuid.Nil {
		return HeartbeatResponseDTO{}, fmt.Errorf("nil uuid provided: %w", ErrInvalidInput)
	}

	contextWorkerID, ok := IDFromContext(ctx)

	if !ok {
		return HeartbeatResponseDTO{}, ErrMissingWorkerID
	}

	if input.WorkerID != contextWorkerID {
		return HeartbeatResponseDTO{}, fmt.Errorf("worker id conflict, context id: %s, provided id: %s: %w", contextWorkerID, input.WorkerID, ErrInvalidInput)
	}

	seenAt := time.Now()
	newLeaseEndTime := seenAt.Add(time.Duration(LeaseSeconds) * time.Second)

	if err := s.workersRepo.ExtendLease(ctx, input.WorkerID, newLeaseEndTime, seenAt); err != nil {
		return HeartbeatResponseDTO{}, fmt.Errorf("extend worker lease: %w", err)
	}

	output := HeartbeatResponseDTO{
		WorkerID:       input.WorkerID,
		LeaseExpiresAt: newLeaseEndTime,
	}

	return output, nil
}
