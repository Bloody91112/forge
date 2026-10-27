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
	worker           Worker
	err              error
	calls            int
	extendedWorkerID uuid.UUID
	leaseExpiresAt   time.Time
	lastSeenAt       time.Time
}

func (r *workerRepositoryStub) Register(_ context.Context, w Worker) error {
	r.calls++
	r.worker = w
	return r.err
}

func (r *workerRepositoryStub) ExtendLease(
	_ context.Context,
	id uuid.UUID,
	leaseExpiresAt time.Time,
	seenAt time.Time,
) error {
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

func (s *bootstrapSettingsStub) GetSettingValue(
	_ context.Context,
	_ string,
) (string, error) {
	s.getCalls++
	return s.value, s.getErr
}

func (*bootstrapSettingsStub) CreateSetting(context.Context, string, string) error {
	return nil
}

func (*bootstrapSettingsStub) UpdateSettingValue(context.Context, string, string) error {
	return nil
}

func newWorkerService(
	repo *workerRepositoryStub,
	settings *bootstrapSettingsStub,
) *Service {
	return NewService(repo, bootstrap.NewService(settings))
}

func validRegisterInput() RegisterRequestDTO {
	return RegisterRequestDTO{
		ID:              uuid.New(),
		ProtocolVersion: RequiredProtocolVersion,
		Capabilities:    json.RawMessage(`["email","webhook"]`),
	}
}

func assertRepositoryCalls(t *testing.T, repo *workerRepositoryStub, want int) {
	t.Helper()

	if repo.calls != want {
		t.Fatalf("repository calls = %d, want %d", repo.calls, want)
	}
}

func assertErrorIs(t *testing.T, err, want error) {
	t.Helper()

	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want errors.Is(_, %v)", err, want)
	}
}

func TestService_Register(t *testing.T) {
	ctx := context.Background()
	const bootstrapToken = "bootstrap-secret"

	t.Run("registers worker and stores only token hash", func(t *testing.T) {
		repo := &workerRepositoryStub{}
		settings := &bootstrapSettingsStub{
			value: token.HashToken(bootstrapToken),
		}
		service := newWorkerService(repo, settings)
		input := validRegisterInput()

		before := time.Now()
		got, err := service.Register(ctx, bootstrapToken, input)
		after := time.Now()

		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}

		assertRepositoryCalls(t, repo, 1)

		if got.ID != input.ID {
			t.Errorf("response ID = %s, want %s", got.ID, input.ID)
		}
		if got.Token == "" {
			t.Fatal("response token is empty")
		}
		if len(got.Token) != 64 {
			t.Errorf("token length = %d, want 64", len(got.Token))
		}
		if got.Token == bootstrapToken {
			t.Error("worker token must differ from bootstrap token")
		}

		if want := token.HashToken(got.Token); repo.worker.TokenHash != want {
			t.Error("repository did not receive the hash of the worker token")
		}
		if repo.worker.TokenHash == got.Token {
			t.Error("repository received the plaintext worker token")
		}
		if repo.worker.ID != input.ID {
			t.Errorf("stored worker ID = %s, want %s", repo.worker.ID, input.ID)
		}
		if repo.worker.ProtocolVersion != RequiredProtocolVersion {
			t.Errorf(
				"stored protocol version = %q, want %q",
				repo.worker.ProtocolVersion,
				RequiredProtocolVersion,
			)
		}

		if diff := cmp.Diff([]string{"email", "webhook"}, repo.worker.Capabilities); diff != "" {
			t.Errorf("capabilities mismatch (-want +got):\n%s", diff)
		}

		if repo.worker.RegisteredAt.Before(before) || repo.worker.RegisteredAt.After(after) {
			t.Errorf("RegisteredAt = %v, outside registration interval", repo.worker.RegisteredAt)
		}

		if !got.LeaseExpiresAt.Equal(repo.worker.LeaseExpiresAt) {
			t.Error("response lease expiry differs from stored lease expiry")
		}

		wantExpiry := repo.worker.RegisteredAt.Add(LeaseSeconds * time.Second)
		if !repo.worker.LeaseExpiresAt.Equal(wantExpiry) {
			t.Errorf("lease expiry = %v, want %v", repo.worker.LeaseExpiresAt, wantExpiry)
		}

		if repo.worker.LastSeenAt != nil {
			t.Errorf("LastSeenAt = %v, want nil", repo.worker.LastSeenAt)
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

				assertErrorIs(t, err, tt.wantError)
				assertRepositoryCalls(t, repo, 0)

				if settings.getCalls != 0 {
					t.Errorf("bootstrap storage reads = %d, want 0", settings.getCalls)
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

		assertErrorIs(t, err, ErrInvalidBootstrapToken)
		assertRepositoryCalls(t, repo, 0)
	})

	t.Run("propagates bootstrap storage error", func(t *testing.T) {
		storageErr := errors.New("storage unavailable")
		repo := &workerRepositoryStub{}
		settings := &bootstrapSettingsStub{getErr: storageErr}
		service := newWorkerService(repo, settings)

		_, err := service.Register(ctx, bootstrapToken, validRegisterInput())

		assertErrorIs(t, err, storageErr)
		assertRepositoryCalls(t, repo, 0)
	})

	t.Run("propagates repository error", func(t *testing.T) {
		repositoryErr := errors.New("database unavailable")
		repo := &workerRepositoryStub{err: repositoryErr}
		settings := &bootstrapSettingsStub{
			value: token.HashToken(bootstrapToken),
		}
		service := newWorkerService(repo, settings)

		_, err := service.Register(ctx, bootstrapToken, validRegisterInput())

		assertErrorIs(t, err, repositoryErr)
		assertRepositoryCalls(t, repo, 1)
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
			t.Errorf("response WorkerID = %s, want %s", got.WorkerID, workerID)
		}

		minExpiry := before.Add(LeaseSeconds * time.Second)
		maxExpiry := after.Add(LeaseSeconds * time.Second)
		if got.LeaseExpiresAt.Before(minExpiry) || got.LeaseExpiresAt.After(maxExpiry) {
			t.Errorf("LeaseExpiresAt = %v, want between %v and %v", got.LeaseExpiresAt, minExpiry, maxExpiry)
		}

		assertRepositoryCalls(t, repo, 1)

		if repo.extendedWorkerID != workerID {
			t.Errorf("repository worker ID = %s, want %s", repo.extendedWorkerID, workerID)
		}
		if repo.lastSeenAt.Before(before) || repo.lastSeenAt.After(after) {
			t.Errorf("last_seen_at = %v, outside service call interval", repo.lastSeenAt)
		}
		if !repo.leaseExpiresAt.Equal(got.LeaseExpiresAt) {
			t.Errorf(
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

		assertErrorIs(t, err, ErrInvalidInput)
		assertRepositoryCalls(t, repo, 0)
	})

	t.Run("rejects worker ID different from authenticated worker", func(t *testing.T) {
		repo := &workerRepositoryStub{}
		service := newWorkerService(repo, &bootstrapSettingsStub{})

		ctx := WithID(context.Background(), workerID)
		_, err := service.ExtendLease(ctx, HeartbeatRequestDTO{
			WorkerID: uuid.New(),
		})

		assertErrorIs(t, err, ErrInvalidInput)
		assertRepositoryCalls(t, repo, 0)
	})

	t.Run("rejects missing authenticated worker ID", func(t *testing.T) {
		repo := &workerRepositoryStub{}
		service := newWorkerService(repo, &bootstrapSettingsStub{})

		_, err := service.ExtendLease(context.Background(), HeartbeatRequestDTO{
			WorkerID: workerID,
		})

		assertErrorIs(t, err, ErrMissingWorkerID)
		assertRepositoryCalls(t, repo, 0)
	})

	t.Run("propagates repository error", func(t *testing.T) {
		repositoryErr := errors.New("database unavailable")
		repo := &workerRepositoryStub{err: repositoryErr}
		service := newWorkerService(repo, &bootstrapSettingsStub{})

		ctx := WithID(context.Background(), workerID)
		_, err := service.ExtendLease(ctx, HeartbeatRequestDTO{
			WorkerID: workerID,
		})

		assertErrorIs(t, err, repositoryErr)
		assertRepositoryCalls(t, repo, 1)
	})
}
