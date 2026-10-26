package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"forge/internal/bootstrap"
	"forge/internal/token"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
)

type workerRepositoryStub struct {
	worker Worker
	err    error
	calls  int

	extendedWorkerID uuid.UUID
	leaseExpiresAt   time.Time
	lastSeenAt       time.Time
}

func (r *workerRepositoryStub) Register(_ context.Context, w Worker) error {
	r.calls++
	r.worker = w
	return r.err
}

func (r *workerRepositoryStub) ExtendLease(ctx context.Context, id uuid.UUID, leaseExpiresAt time.Time, seenAt time.Time) error {
	r.calls++
	r.extendedWorkerID = id
	r.leaseExpiresAt = leaseExpiresAt
	r.lastSeenAt = seenAt
	return r.err
}

type bootstrapSettingsStub struct {
	value    string
	getErr   error
	getCalls int
}

func (s *bootstrapSettingsStub) GetSettingValue(ctx context.Context, key string) (string, error) {
	s.getCalls++
	return s.value, s.getErr
}

func (*bootstrapSettingsStub) CreateSetting(ctx context.Context, key string, value string) error {
	return nil
}

func (*bootstrapSettingsStub) UpdateSettingValue(ctx context.Context, key string, value string) error {
	return nil
}

func newWorkerService(repo *workerRepositoryStub, settings *bootstrapSettingsStub) *Service {
	return NewService(repo, bootstrap.NewService(settings))
}

func validRegisterInput() RegisterRequestDTO {
	return RegisterRequestDTO{
		ID:              uuid.New(),
		ProtocolVersion: RequiredProtocolVersion,
		Capabilities:    json.RawMessage(`["email","webhook"]`),
	}
}

func TestService_Register(t *testing.T) {
	ctx := context.Background()
	const bootstrapToken = "bootstrap-secret"

	t.Run("registers worker and stores only token hash", func(t *testing.T) {
		repo := &workerRepositoryStub{}
		settings := &bootstrapSettingsStub{value: token.HashToken(bootstrapToken)}
		service := newWorkerService(repo, settings)
		input := validRegisterInput()

		before := time.Now()
		got, err := service.Register(ctx, bootstrapToken, input)
		after := time.Now()

		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}

		if repo.calls != 1 {
			t.Fatalf("repository calls = %d, want 1", repo.calls)
		}

		if got.ID != input.ID {
			t.Fatalf("output ID = %s, want %s", got.ID, input.ID)
		}

		if got.Token == "" {
			t.Fatal("worker token is empty")
		}

		if len(got.Token) != 64 {
			t.Fatalf("worker token length = %d, want 64 hex characters", len(got.Token))
		}

		if got.Token == bootstrapToken {
			t.Fatal("worker token must differ from bootstrap token")
		}

		if repo.worker.TokenHash != token.HashToken(got.Token) {
			t.Fatal("repository did not receive the hash of the returned worker token")
		}

		if repo.worker.TokenHash == got.Token {
			t.Fatal("repository received the plaintext worker token")
		}

		if repo.worker.ID != input.ID {
			t.Fatalf("stored worker ID = %s, want %s", repo.worker.ID, input.ID)
		}

		if repo.worker.ProtocolVersion != RequiredProtocolVersion {
			t.Fatalf(
				"stored protocol version = %q, want %q",
				repo.worker.ProtocolVersion,
				RequiredProtocolVersion,
			)
		}

		if diff := cmp.Diff([]string{"email", "webhook"}, repo.worker.Capabilities); diff != "" {
			t.Fatalf("stored capabilities mismatch (-want +got):\n%s", diff)
		}

		if repo.worker.RegisteredAt.Before(before) || repo.worker.RegisteredAt.After(after) {
			t.Fatalf("RegisteredAt %v is outside the registration interval", repo.worker.RegisteredAt)
		}

		if got.LeaseExpiresAt != repo.worker.LeaseExpiresAt {
			t.Fatal("output lease expiry differs from stored lease expiry")
		}

		if want := repo.worker.RegisteredAt.Add(LeaseSeconds * time.Second); !repo.worker.LeaseExpiresAt.Equal(want) {
			t.Fatalf("lease expiry = %v, want %v", repo.worker.LeaseExpiresAt, want)
		}

		if repo.worker.LastSeenAt != nil {
			t.Fatal("LastSeenAt must be nil for a newly registered worker")
		}
	})

	t.Run("rejects invalid input before token validation or persistence", func(t *testing.T) {
		tests := []struct {
			name      string
			token     string
			input     RegisterRequestDTO
			wantError error
		}{
			{
				name:      "empty bootstrap token",
				token:     "",
				input:     validRegisterInput(),
				wantError: ErrInvalidInput,
			},
			{
				name:  "nil worker ID",
				token: bootstrapToken,
				input: RegisterRequestDTO{
					ProtocolVersion: RequiredProtocolVersion,
					Capabilities:    json.RawMessage(`[]`),
				},
				wantError: ErrInvalidInput,
			},
			{
				name:  "unsupported protocol",
				token: bootstrapToken,
				input: RegisterRequestDTO{
					ID:              uuid.New(),
					ProtocolVersion: "2",
					Capabilities:    json.RawMessage(`[]`),
				},
				wantError: ErrUnsupportedProtocolVersion,
			},
			{
				name:  "capabilities must be an array",
				token: bootstrapToken,
				input: RegisterRequestDTO{
					ID:              uuid.New(),
					ProtocolVersion: RequiredProtocolVersion,
					Capabilities:    json.RawMessage(`{"email":true}`),
				},
				wantError: ErrInvalidInput,
			},
			{
				name:  "capabilities cannot be null",
				token: bootstrapToken,
				input: RegisterRequestDTO{
					ID:              uuid.New(),
					ProtocolVersion: RequiredProtocolVersion,
					Capabilities:    json.RawMessage(`null`),
				},
				wantError: ErrInvalidInput,
			},
			{
				name:  "capability cannot be blank",
				token: bootstrapToken,
				input: RegisterRequestDTO{
					ID:              uuid.New(),
					ProtocolVersion: RequiredProtocolVersion,
					Capabilities:    json.RawMessage(`["   "]`),
				},
				wantError: ErrInvalidInput,
			},
			{
				name:  "capability cannot have surrounding whitespace",
				token: bootstrapToken,
				input: RegisterRequestDTO{
					ID:              uuid.New(),
					ProtocolVersion: RequiredProtocolVersion,
					Capabilities:    json.RawMessage(`[" email"]`),
				},
				wantError: ErrInvalidInput,
			},
			{
				name:  "capabilities cannot contain duplicates",
				token: bootstrapToken,
				input: RegisterRequestDTO{
					ID:              uuid.New(),
					ProtocolVersion: RequiredProtocolVersion,
					Capabilities:    json.RawMessage(`["email","email"]`),
				},
				wantError: ErrInvalidInput,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				repo := &workerRepositoryStub{}
				settings := &bootstrapSettingsStub{
					value: token.HashToken(bootstrapToken),
				}
				service := newWorkerService(repo, settings)

				_, err := service.Register(ctx, tt.token, tt.input)

				if !errors.Is(err, tt.wantError) {
					t.Fatalf("Register() error = %v, want errors.Is(_, %v)", err, tt.wantError)
				}

				if repo.calls != 0 {
					t.Fatalf("repository calls = %d, want 0", repo.calls)
				}

				if settings.getCalls != 0 {
					t.Fatalf("bootstrap storage reads = %d, want 0", settings.getCalls)
				}
			})
		}
	})

	t.Run("rejects incorrect bootstrap token", func(t *testing.T) {
		repo := &workerRepositoryStub{}
		settings := &bootstrapSettingsStub{
			value: token.HashToken("different-token"),
		}
		service := newWorkerService(repo, settings)

		_, err := service.Register(ctx, bootstrapToken, validRegisterInput())

		if !errors.Is(err, ErrInvalidBootstrapToken) {
			t.Fatalf("Register() error = %v, want ErrInvalidBootstrapToken", err)
		}

		if repo.calls != 0 {
			t.Fatalf("repository calls = %d, want 0", repo.calls)
		}
	})

	t.Run("propagates bootstrap storage error", func(t *testing.T) {
		storageErr := errors.New("storage unavailable")
		repo := &workerRepositoryStub{}
		settings := &bootstrapSettingsStub{getErr: storageErr}
		service := newWorkerService(repo, settings)

		_, err := service.Register(ctx, bootstrapToken, validRegisterInput())

		if !errors.Is(err, storageErr) {
			t.Fatalf("Register() error = %v, want wrapped storage error", err)
		}

		if repo.calls != 0 {
			t.Fatalf("repository calls = %d, want 0", repo.calls)
		}
	})

	t.Run("propagates repository error", func(t *testing.T) {
		repositoryErr := errors.New("database unavailable")
		repo := &workerRepositoryStub{err: repositoryErr}
		settings := &bootstrapSettingsStub{
			value: token.HashToken(bootstrapToken),
		}
		service := newWorkerService(repo, settings)

		_, err := service.Register(ctx, bootstrapToken, validRegisterInput())

		if !errors.Is(err, repositoryErr) {
			t.Fatalf("Register() error = %v, want wrapped repository error", err)
		}

		if repo.calls != 1 {
			t.Fatalf("repository calls = %d, want 1", repo.calls)
		}
	})
}

func TestService_ExtendLease(t *testing.T) {
	workerID := uuid.New()

	t.Run("extends registration lease", func(t *testing.T) {
		repo := &workerRepositoryStub{}
		service := newWorkerService(repo, &bootstrapSettingsStub{})

		ctx := WithID(context.Background(), workerID)
		before := time.Now()

		got, err := service.ExtendLease(ctx, HeartbeatRequestDTO{
			WorkerID: workerID,
		})
		after := time.Now()

		if err != nil {
			t.Fatalf("ExtendLease() error = %v", err)
		}

		if got.WorkerID != workerID {
			t.Fatalf("WorkerID = %s, want %s", got.WorkerID, workerID)
		}

		if got.LeaseExpiresAt.Before(before.Add(LeaseSeconds*time.Second)) ||
			got.LeaseExpiresAt.After(after.Add(LeaseSeconds*time.Second)) {
			t.Fatalf(
				"LeaseExpiresAt = %v, want approximately now + %d seconds",
				got.LeaseExpiresAt,
				LeaseSeconds,
			)
		}

		if repo.calls != 1 {
			t.Fatalf("repository calls = %d, want 1", repo.calls)
		}

		if repo.extendedWorkerID != workerID {
			t.Fatalf(
				"repository worker ID = %s, want %s",
				repo.extendedWorkerID,
				workerID,
			)
		}

		if repo.lastSeenAt.Before(before) || repo.lastSeenAt.After(after) {
			t.Fatalf("last_seen_at = %v, want timestamp within service call", repo.lastSeenAt)
		}

		if !repo.leaseExpiresAt.Equal(got.LeaseExpiresAt) {
			t.Fatalf(
				"repository lease expiry = %v, response expiry = %v",
				repo.leaseExpiresAt,
				got.LeaseExpiresAt,
			)
		}
	})

	t.Run("rejects nil worker ID", func(t *testing.T) {
		repo := &workerRepositoryStub{}
		service := newWorkerService(repo, &bootstrapSettingsStub{})

		ctx := WithID(context.Background(), workerID)
		_, err := service.ExtendLease(ctx, HeartbeatRequestDTO{})

		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("ExtendLease() error = %v, want ErrInvalidInput", err)
		}

		if repo.calls != 0 {
			t.Fatalf("repository calls = %d, want 0", repo.calls)
		}
	})

	t.Run("rejects worker ID different from authenticated worker", func(t *testing.T) {
		repo := &workerRepositoryStub{}
		service := newWorkerService(repo, &bootstrapSettingsStub{})

		ctx := WithID(context.Background(), workerID)
		_, err := service.ExtendLease(ctx, HeartbeatRequestDTO{
			WorkerID: uuid.New(),
		})

		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("ExtendLease() error = %v, want ErrInvalidInput", err)
		}

		if repo.calls != 0 {
			t.Fatalf("repository calls = %d, want 0", repo.calls)
		}
	})

	t.Run("rejects missing authenticated worker ID", func(t *testing.T) {
		repo := &workerRepositoryStub{}
		service := newWorkerService(repo, &bootstrapSettingsStub{})

		_, err := service.ExtendLease(context.Background(), HeartbeatRequestDTO{
			WorkerID: workerID,
		})

		if !errors.Is(err, ErrMissingWorkerID) {
			t.Fatalf("ExtendLease() error = %v, want ErrMissingWorkerID", err)
		}

		if repo.calls != 0 {
			t.Fatalf("repository calls = %d, want 0", repo.calls)
		}
	})

	t.Run("propagates repository error", func(t *testing.T) {
		repositoryErr := errors.New("database unavailable")
		repo := &workerRepositoryStub{err: repositoryErr}
		service := newWorkerService(repo, &bootstrapSettingsStub{})

		ctx := WithID(context.Background(), workerID)
		_, err := service.ExtendLease(ctx, HeartbeatRequestDTO{
			WorkerID: workerID,
		})

		if !errors.Is(err, repositoryErr) {
			t.Fatalf("ExtendLease() error = %v, want wrapped repository error", err)
		}

		if repo.calls != 1 {
			t.Fatalf("repository calls = %d, want 1", repo.calls)
		}
	})
}
