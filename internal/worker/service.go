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

var ErrInvalidInput = errors.New("invalid worker input")
var ErrInvalidBootstrapToken = errors.New("invalid bootstrap token")
var ErrUnsupportedProtocolVersion = errors.New("unsupported protocol version")

const LeaseSeconds = 30
const RequiredProtocolVersion = "1"

type Repository interface {
	Register(ctx context.Context, worker Worker) error
}

type Service struct {
	workersRepo      Repository
	bootstrapService bootstrap.Service
}

func NewService(workersRepo Repository, bootstrapService bootstrap.Service) *Service {
	return &Service{
		workersRepo:      workersRepo,
		bootstrapService: bootstrapService,
	}
}

func (s *Service) Register(ctx context.Context, bootstrapToken string, input RegisterWorkerInput) (RegisterWorkerOutput, error) {
	if bootstrapToken == "" {
		return RegisterWorkerOutput{}, fmt.Errorf("bootstrap token empty string provided: %w", ErrInvalidInput)
	}

	if input.ID == uuid.Nil {
		return RegisterWorkerOutput{}, fmt.Errorf("nil uuid provided: %w", ErrInvalidInput)
	}

	if input.ProtocolVersion != RequiredProtocolVersion {
		return RegisterWorkerOutput{}, fmt.Errorf("unsupported protocol version %q: %w", input.ProtocolVersion, ErrUnsupportedProtocolVersion)
	}

	capabilities, err := extractWorkerCapabilities(input.Capabilities)
	if err != nil {
		return RegisterWorkerOutput{}, err
	}

	tokenOk, err := s.bootstrapService.Validate(ctx, bootstrapToken)
	if err != nil {
		return RegisterWorkerOutput{}, fmt.Errorf("failed to validate token, err: %w", err)
	}

	if !tokenOk {
		return RegisterWorkerOutput{}, ErrInvalidBootstrapToken
	}

	workerToken, err := token.GenerateWorkerToken()

	if err != nil {
		return RegisterWorkerOutput{}, fmt.Errorf("generate worker token: %w", err)
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
		return RegisterWorkerOutput{}, fmt.Errorf("failed to register worker: %w", err)
	}

	output := RegisterWorkerOutput{
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
