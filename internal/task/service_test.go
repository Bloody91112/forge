package task

import (
	"context"
	"errors"
	"forge/internal/worker"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
)

type taskRepositoryStub struct {
	task Task

	lookupTask   *Task
	lookupErr    error
	lookupCalled bool
	lookupClient uuid.UUID
	lookupKey    string

	getByIDErr    error
	getByIDCalled bool
	gotTaskID     uuid.UUID
	gotClientID   uuid.UUID

	createTask    *Task
	createCreated bool
	createErr     error
	createCalled  bool

	claimTask         *Task
	claimErr          error
	claimCalled       bool
	claimWorkerID     uuid.UUID
	claimCapabilities []string
	claimLeaseSeconds int
}

func (r *taskRepositoryStub) CreateOrGetByIdempotencyKey(
	_ context.Context,
	task Task,
) (Task, bool, error) {
	r.createCalled = true

	if r.createErr != nil {
		return Task{}, false, r.createErr
	}

	if r.createTask != nil {
		return *r.createTask, r.createCreated, nil
	}

	return task, true, nil
}

func (r *taskRepositoryStub) GetByID(
	_ context.Context,
	taskID uuid.UUID,
	clientID uuid.UUID,
) (Task, error) {
	r.getByIDCalled = true
	r.gotTaskID = taskID
	r.gotClientID = clientID

	if r.getByIDErr != nil {
		return Task{}, r.getByIDErr
	}

	return r.task, nil
}

func (r *taskRepositoryStub) GetByIdempotencyKey(
	_ context.Context,
	clientID uuid.UUID,
	key string,
) (Task, error) {
	r.lookupCalled = true
	r.lookupClient = clientID
	r.lookupKey = key

	if r.lookupErr != nil {
		return Task{}, r.lookupErr
	}

	if r.lookupTask != nil {
		return *r.lookupTask, nil
	}

	return Task{}, ErrTaskNotFound
}

func (r *taskRepositoryStub) FindAndClaimForWorker(
	_ context.Context,
	workerID uuid.UUID,
	capabilities []string,
	leaseSeconds int,
) (Task, error) {
	r.claimCalled = true
	r.claimWorkerID = workerID
	r.claimCapabilities = capabilities
	r.claimLeaseSeconds = leaseSeconds

	if r.claimErr != nil {
		return Task{}, r.claimErr
	}

	if r.claimTask != nil {
		return *r.claimTask, nil
	}

	return Task{}, ErrNoAvailableTasks
}

type workerRepositoryStub struct {
	capabilities                [][]string
	err                         error
	called                      bool
	workerCapabilities          []string
	getWorkerCapabilitiesErr    error
	getWorkerCapabilitiesCalled bool
	gotWorkerID                 uuid.UUID
}

func (r *workerRepositoryStub) GetActiveWorkersCapabilities(
	_ context.Context,
) ([][]string, error) {
	r.called = true

	if r.err != nil {
		return nil, r.err
	}

	return r.capabilities, nil
}

func (r *workerRepositoryStub) GetActiveWorkerCapabilities(
	_ context.Context,
	workerID uuid.UUID,
) ([]string, error) {
	r.getWorkerCapabilitiesCalled = true
	r.gotWorkerID = workerID

	if r.getWorkerCapabilitiesErr != nil {
		return nil, r.getWorkerCapabilitiesErr
	}

	return r.workerCapabilities, nil
}

func newServiceWithCompatibleWorker(
	repo *taskRepositoryStub,
	taskType string,
) (*Service, *workerRepositoryStub) {
	workersRepo := &workerRepositoryStub{
		capabilities: [][]string{
			{taskType},
		},
	}

	return NewService(repo, workersRepo), workersRepo
}

func validCreateTaskInput() CreateTaskInput {
	maxAttempts := int64(3)
	timeoutSeconds := int64(22)

	return CreateTaskInput{
		Type:           "email-send",
		Payload:        []byte(`{"user_id":42}`),
		IdempotencyKey: uuid.NewString(),
		MaxAttempts:    &maxAttempts,
		TimeoutSeconds: &timeoutSeconds,
	}
}

func validCreateContext(t *testing.T) context.Context {
	t.Helper()

	return WithClientID(t.Context(), uuid.New())
}

func expectedTaskForInput(
	t *testing.T,
	input CreateTaskInput,
	clientID uuid.UUID,
) Task {
	t.Helper()

	maxAttempts := int64(DefaultTaskMaxAttempts)
	if input.MaxAttempts != nil {
		maxAttempts = *input.MaxAttempts
	}

	timeoutSeconds := int64(DefaultTaskTimeout)
	if input.TimeoutSeconds != nil {
		timeoutSeconds = *input.TimeoutSeconds
	}

	hash, err := calculateRequestHash(
		input.Payload,
		input.Type,
		timeoutSeconds,
		maxAttempts,
	)
	if err != nil {
		t.Fatalf("calculateRequestHash() error = %v", err)
	}

	return Task{
		ID:             uuid.New(),
		ClientID:       clientID,
		IdempotencyKey: input.IdempotencyKey,
		RequestHash:    hash,
		Type:           input.Type,
		Payload:        input.Payload,
		Status:         StatusQueued,
		MaxAttempts:    maxAttempts,
		TimeoutSeconds: timeoutSeconds,
	}
}

func TestService_GetByID_TaskExists(t *testing.T) {
	taskID := uuid.New()
	clientID := uuid.New()

	repo := &taskRepositoryStub{
		task: Task{ID: taskID},
	}
	service := NewService(repo, &workerRepositoryStub{})
	ctx := WithClientID(t.Context(), clientID)

	got, err := service.GetByID(ctx, taskID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if got.ID != taskID {
		t.Errorf("task ID = %s, want %s", got.ID, taskID)
	}

	if !repo.getByIDCalled {
		t.Fatal("expected repository GetByID to be called")
	}

	if repo.gotTaskID != taskID {
		t.Errorf("repository task ID = %s, want %s", repo.gotTaskID, taskID)
	}

	if repo.gotClientID != clientID {
		t.Errorf("repository client ID = %s, want %s", repo.gotClientID, clientID)
	}
}

func TestService_GetByID_TaskNotFound(t *testing.T) {
	repo := &taskRepositoryStub{
		getByIDErr: ErrTaskNotFound,
	}
	service := NewService(repo, &workerRepositoryStub{})
	ctx := WithClientID(t.Context(), uuid.New())

	_, err := service.GetByID(ctx, uuid.New())
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("error = %v, want %v", err, ErrTaskNotFound)
	}
}

func TestService_GetByID_WithoutClientID(t *testing.T) {
	repo := &taskRepositoryStub{}
	service := NewService(repo, &workerRepositoryStub{})

	_, err := service.GetByID(t.Context(), uuid.New())
	if !errors.Is(err, ErrMissingClientID) {
		t.Fatalf("error = %v, want %v", err, ErrMissingClientID)
	}

	if repo.getByIDCalled {
		t.Fatal("repository GetByID must not be called without client ID")
	}
}

func TestService_Create(t *testing.T) {
	input := validCreateTaskInput()
	clientID := uuid.New()
	ctx := WithClientID(t.Context(), clientID)

	repo := &taskRepositoryStub{}
	service, workersRepo := newServiceWithCompatibleWorker(repo, input.Type)

	got, err := service.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	want := expectedTaskForInput(t, input, clientID)

	if diff := cmp.Diff(want, got.Task, cmpopts.IgnoreFields(Task{}, "ID")); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}

	if !got.Created {
		t.Error("Created = false, want true")
	}

	if !repo.lookupCalled {
		t.Error("expected GetByIdempotencyKey to be called")
	}

	if repo.lookupClient != clientID {
		t.Errorf("lookup client ID = %s, want %s", repo.lookupClient, clientID)
	}

	if repo.lookupKey != input.IdempotencyKey {
		t.Errorf("lookup key = %q, want %q", repo.lookupKey, input.IdempotencyKey)
	}

	if !workersRepo.called {
		t.Error("expected active worker capabilities to be checked")
	}

	if !repo.createCalled {
		t.Error("expected CreateOrGetByIdempotencyKey to be called")
	}
}

func TestService_CreateWithDefaultFields(t *testing.T) {
	input := validCreateTaskInput()
	input.MaxAttempts = nil
	input.TimeoutSeconds = nil

	clientID := uuid.New()
	repo := &taskRepositoryStub{}
	service, _ := newServiceWithCompatibleWorker(repo, input.Type)

	got, err := service.Create(WithClientID(t.Context(), clientID), input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	want := expectedTaskForInput(t, input, clientID)

	if diff := cmp.Diff(want, got.Task, cmpopts.IgnoreFields(Task{}, "ID")); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}

	if !got.Created {
		t.Error("Created = false, want true")
	}
}

func TestService_CreateExistingIdempotencyKeyReturnsTaskWithoutCheckingWorkers(t *testing.T) {
	input := validCreateTaskInput()
	clientID := uuid.New()
	existingTask := expectedTaskForInput(t, input, clientID)

	repo := &taskRepositoryStub{
		lookupTask: &existingTask,
	}
	workersRepo := &workerRepositoryStub{}
	service := NewService(repo, workersRepo)

	got, err := service.Create(WithClientID(t.Context(), clientID), input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if diff := cmp.Diff(existingTask, got.Task); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}

	if got.Created {
		t.Error("Created = true, want false for existing task")
	}

	if workersRepo.called {
		t.Error("worker pool must not be checked for an existing idempotent request")
	}

	if repo.createCalled {
		t.Error("task must not be created again")
	}
}

func TestService_CreateExistingIdempotencyKeyWithDifferentHashReturnsConflict(t *testing.T) {
	input := validCreateTaskInput()
	clientID := uuid.New()
	existingTask := expectedTaskForInput(t, input, clientID)
	existingTask.RequestHash = "different-hash"

	repo := &taskRepositoryStub{
		lookupTask: &existingTask,
	}
	workersRepo := &workerRepositoryStub{}
	service := NewService(repo, workersRepo)

	got, err := service.Create(WithClientID(t.Context(), clientID), input)
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("error = %v, want %v", err, ErrIdempotencyConflict)
	}

	if diff := cmp.Diff(existingTask, got.Task); diff != "" {
		t.Errorf("returned task mismatch (-want +got):\n%s", diff)
	}

	if got.Created {
		t.Error("Created = true, want false for idempotency conflict")
	}

	if workersRepo.called {
		t.Error("worker pool must not be checked for an idempotency conflict")
	}

	if repo.createCalled {
		t.Error("task must not be created after an idempotency conflict")
	}
}

func TestService_CreateWithConcurrentIdempotencyConflict(t *testing.T) {
	input := validCreateTaskInput()
	clientID := uuid.New()
	existingTask := expectedTaskForInput(t, input, clientID)

	repo := &taskRepositoryStub{
		createTask: &existingTask,
	}
	service, _ := newServiceWithCompatibleWorker(repo, input.Type)

	got, err := service.Create(WithClientID(t.Context(), clientID), input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if diff := cmp.Diff(existingTask, got.Task); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}

	if got.Created {
		t.Error("Created = true, want false for concurrent idempotent request")
	}

	if !repo.createCalled {
		t.Fatal("expected CreateOrGetByIdempotencyKey to be called")
	}
}

func TestService_CreateWithConcurrentDifferentRequestReturnsConflict(t *testing.T) {
	input := validCreateTaskInput()
	clientID := uuid.New()
	existingTask := expectedTaskForInput(t, input, clientID)
	existingTask.RequestHash = "different-hash"

	repo := &taskRepositoryStub{
		createTask: &existingTask,
	}
	service, _ := newServiceWithCompatibleWorker(repo, input.Type)

	got, err := service.Create(WithClientID(t.Context(), clientID), input)
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("error = %v, want %v", err, ErrIdempotencyConflict)
	}

	if diff := cmp.Diff(existingTask, got); diff != "" {
		t.Errorf("returned task mismatch (-want +got):\n%s", diff)
	}
}

func TestService_CreateWithoutActiveWorkers(t *testing.T) {
	input := validCreateTaskInput()
	repo := &taskRepositoryStub{}
	workersRepo := &workerRepositoryStub{}
	service := NewService(repo, workersRepo)

	_, err := service.Create(validCreateContext(t), input)
	if !errors.Is(err, ErrNoActiveWorkers) {
		t.Fatalf("error = %v, want %v", err, ErrNoActiveWorkers)
	}

	if !workersRepo.called {
		t.Fatal("expected worker repository to be called")
	}

	if repo.createCalled {
		t.Error("task must not be created when no active workers exist")
	}
}

func TestService_CreateRejectsTaskWhenAnyActiveWorkerIsIncompatible(t *testing.T) {
	input := validCreateTaskInput()
	repo := &taskRepositoryStub{}
	workersRepo := &workerRepositoryStub{
		capabilities: [][]string{
			{input.Type, "image-resize"},
			{"report-generate"},
		},
	}
	service := NewService(repo, workersRepo)

	_, err := service.Create(validCreateContext(t), input)
	if !errors.Is(err, ErrTaskTypeNotSupported) {
		t.Fatalf("error = %v, want %v", err, ErrTaskTypeNotSupported)
	}

	if repo.createCalled {
		t.Error("task must not be created when an active worker is incompatible")
	}
}

func TestService_CreateAcceptsTaskWhenAllActiveWorkersAreCompatible(t *testing.T) {
	input := validCreateTaskInput()
	repo := &taskRepositoryStub{}
	workersRepo := &workerRepositoryStub{
		capabilities: [][]string{
			{input.Type},
			{input.Type, "image-resize"},
			{"report-generate", input.Type},
		},
	}
	service := NewService(repo, workersRepo)

	got, err := service.Create(validCreateContext(t), input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if got.Task.Type != input.Type {
		t.Errorf("task type = %q, want %q", got.Task.Type, input.Type)
	}

	if !got.Created {
		t.Error("Created = false, want true")
	}

	if !repo.createCalled {
		t.Fatal("expected task repository to be called")
	}
}

func TestService_CreatePropagatesIdempotencyLookupError(t *testing.T) {
	input := validCreateTaskInput()
	lookupErr := errors.New("database unavailable")

	repo := &taskRepositoryStub{
		lookupErr: lookupErr,
	}
	workersRepo := &workerRepositoryStub{}
	service := NewService(repo, workersRepo)

	_, err := service.Create(validCreateContext(t), input)
	if !errors.Is(err, lookupErr) {
		t.Fatalf("error = %v, want wrapped %v", err, lookupErr)
	}

	if workersRepo.called {
		t.Error("worker repository must not be called after lookup failure")
	}

	if repo.createCalled {
		t.Error("task must not be created after lookup failure")
	}
}

func TestService_CreatePropagatesWorkerRepositoryError(t *testing.T) {
	input := validCreateTaskInput()
	workersErr := errors.New("database unavailable")

	repo := &taskRepositoryStub{}
	workersRepo := &workerRepositoryStub{err: workersErr}
	service := NewService(repo, workersRepo)

	_, err := service.Create(validCreateContext(t), input)
	if !errors.Is(err, workersErr) {
		t.Fatalf("error = %v, want wrapped %v", err, workersErr)
	}

	if repo.createCalled {
		t.Error("task must not be created after worker repository failure")
	}
}

func TestService_CreatePropagatesCreateError(t *testing.T) {
	input := validCreateTaskInput()
	createErr := errors.New("insert failed")

	repo := &taskRepositoryStub{createErr: createErr}
	service, _ := newServiceWithCompatibleWorker(repo, input.Type)

	_, err := service.Create(validCreateContext(t), input)
	if !errors.Is(err, createErr) {
		t.Fatalf("error = %v, want wrapped %v", err, createErr)
	}
}

func TestService_Create_WithoutClientID(t *testing.T) {
	input := validCreateTaskInput()
	repo := &taskRepositoryStub{}
	workersRepo := &workerRepositoryStub{}
	service := NewService(repo, workersRepo)

	_, err := service.Create(t.Context(), input)
	if !errors.Is(err, ErrMissingClientID) {
		t.Fatalf("error = %v, want %v", err, ErrMissingClientID)
	}

	if repo.lookupCalled {
		t.Error("repository must not be called without client ID")
	}

	if workersRepo.called {
		t.Error("worker repository must not be called without client ID")
	}
}

func TestService_Create_ValidatesLimits(t *testing.T) {
	tests := []struct {
		name           string
		maxAttempts    int64
		timeoutSeconds int64
		wantErr        bool
	}{
		{
			name:           "minimum values",
			maxAttempts:    1,
			timeoutSeconds: 1,
		},
		{
			name:           "maximum values",
			maxAttempts:    MaxTaskAttempts,
			timeoutSeconds: MaxTaskTimeout,
		},
		{
			name:           "max attempts below minimum",
			maxAttempts:    0,
			timeoutSeconds: DefaultTaskTimeout,
			wantErr:        true,
		},
		{
			name:           "max attempts above maximum",
			maxAttempts:    MaxTaskAttempts + 1,
			timeoutSeconds: DefaultTaskTimeout,
			wantErr:        true,
		},
		{
			name:           "timeout below minimum",
			maxAttempts:    DefaultTaskMaxAttempts,
			timeoutSeconds: 0,
			wantErr:        true,
		},
		{
			name:           "timeout above maximum",
			maxAttempts:    DefaultTaskMaxAttempts,
			timeoutSeconds: MaxTaskTimeout + 1,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validCreateTaskInput()
			input.MaxAttempts = &tt.maxAttempts
			input.TimeoutSeconds = &tt.timeoutSeconds

			repo := &taskRepositoryStub{}
			service, _ := newServiceWithCompatibleWorker(repo, input.Type)

			_, err := service.Create(validCreateContext(t), input)

			if tt.wantErr {
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("error = %v, want %v", err, ErrInvalidInput)
				}
				return
			}

			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
		})
	}
}

func TestService_Create_ValidatesRequiredFields(t *testing.T) {
	tests := []struct {
		name  string
		input func() CreateTaskInput
	}{
		{
			name: "empty task type",
			input: func() CreateTaskInput {
				input := validCreateTaskInput()
				input.Type = ""
				return input
			},
		},
		{
			name: "empty idempotency key",
			input: func() CreateTaskInput {
				input := validCreateTaskInput()
				input.IdempotencyKey = ""
				return input
			},
		},
		{
			name: "empty payload",
			input: func() CreateTaskInput {
				input := validCreateTaskInput()
				input.Payload = nil
				return input
			},
		},
		{
			name: "invalid JSON payload",
			input: func() CreateTaskInput {
				input := validCreateTaskInput()
				input.Payload = []byte(`{"user_id":`)
				return input
			},
		},
		{
			name: "JSON array payload",
			input: func() CreateTaskInput {
				input := validCreateTaskInput()
				input.Payload = []byte(`[1,2,3]`)
				return input
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &taskRepositoryStub{}
			workersRepo := &workerRepositoryStub{}
			service := NewService(repo, workersRepo)

			_, err := service.Create(validCreateContext(t), tt.input())
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want %v", err, ErrInvalidInput)
			}

			if repo.lookupCalled {
				t.Error("repository must not be called for invalid input")
			}

			if workersRepo.called {
				t.Error("worker repository must not be called for invalid input")
			}
		})
	}
}

func TestService_CreateRequestHashIncludesAllRequestFields(t *testing.T) {
	base := validCreateTaskInput()
	baseHash, err := calculateRequestHash(
		base.Payload,
		base.Type,
		*base.TimeoutSeconds,
		*base.MaxAttempts,
	)
	if err != nil {
		t.Fatalf("calculateRequestHash() error = %v", err)
	}

	tests := []struct {
		name  string
		input func() CreateTaskInput
	}{
		{
			name: "different payload",
			input: func() CreateTaskInput {
				input := base
				input.Payload = []byte(`{"user_id":43}`)
				return input
			},
		},
		{
			name: "different type",
			input: func() CreateTaskInput {
				input := base
				input.Type = "report-generate"
				return input
			},
		},
		{
			name: "different timeout",
			input: func() CreateTaskInput {
				input := base
				timeout := *base.TimeoutSeconds + 1
				input.TimeoutSeconds = &timeout
				return input
			},
		},
		{
			name: "different max attempts",
			input: func() CreateTaskInput {
				input := base
				attempts := *base.MaxAttempts + 1
				input.MaxAttempts = &attempts
				return input
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := tt.input()
			hash, err := calculateRequestHash(
				input.Payload,
				input.Type,
				*input.TimeoutSeconds,
				*input.MaxAttempts,
			)
			if err != nil {
				t.Fatalf("calculateRequestHash() error = %v", err)
			}

			if hash == baseHash {
				t.Fatal("expected request hash to change")
			}
		})
	}
}

func TestService_Claim(t *testing.T) {
	workerID := uuid.New()
	leaseExpiresAt := time.Now().Add(time.Duration(LeaseSeconds) * time.Second)

	expected := Task{
		ID:             uuid.New(),
		Type:           "email-send",
		Payload:        []byte(`{"user_id":42}`),
		Status:         StatusRunning,
		AttemptCount:   1,
		TimeoutSeconds: 22,
		LeaseOwner:     workerID,
		LeaseToken:     uuid.New(),
		LeaseExpiresAt: &leaseExpiresAt,
	}

	repo := &taskRepositoryStub{claimTask: &expected}
	workersRepo := &workerRepositoryStub{
		workerCapabilities: []string{"email-send", "report-generate"},
	}
	service := NewService(repo, workersRepo)

	ctx := worker.WithID(t.Context(), workerID)

	got, err := service.Claim(ctx, ClaimTaskInput{WorkerID: workerID})
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}

	if diff := cmp.Diff(expected, got); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}

	if !workersRepo.getWorkerCapabilitiesCalled {
		t.Error("expected worker capabilities to be fetched")
	}

	if workersRepo.gotWorkerID != workerID {
		t.Errorf("worker repository ID = %s, want %s", workersRepo.gotWorkerID, workerID)
	}

	if !repo.claimCalled {
		t.Fatal("expected FindAndClaimForWorker to be called")
	}

	if repo.claimWorkerID != workerID {
		t.Errorf("claimed worker ID = %s, want %s", repo.claimWorkerID, workerID)
	}

	if diff := cmp.Diff(workersRepo.workerCapabilities, repo.claimCapabilities); diff != "" {
		t.Errorf("capabilities mismatch (-want +got):\n%s", diff)
	}

	if repo.claimLeaseSeconds != LeaseSeconds {
		t.Errorf("lease seconds = %d, want %d", repo.claimLeaseSeconds, LeaseSeconds)
	}
}

func TestService_ClaimRejectsNilWorkerID(t *testing.T) {
	repo := &taskRepositoryStub{}
	workersRepo := &workerRepositoryStub{}
	service := NewService(repo, workersRepo)

	_, err := service.Claim(t.Context(), ClaimTaskInput{})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidInput)
	}

	if workersRepo.getWorkerCapabilitiesCalled {
		t.Error("worker repository must not be called")
	}

	if repo.claimCalled {
		t.Error("task repository must not be called")
	}
}

func TestService_ClaimWithoutWorkerIDInContext(t *testing.T) {
	repo := &taskRepositoryStub{}
	workersRepo := &workerRepositoryStub{}
	service := NewService(repo, workersRepo)

	_, err := service.Claim(
		t.Context(),
		ClaimTaskInput{WorkerID: uuid.New()},
	)
	if !errors.Is(err, worker.ErrMissingWorkerID) {
		t.Fatalf("error = %v, want %v", err, worker.ErrMissingWorkerID)
	}

	if workersRepo.getWorkerCapabilitiesCalled {
		t.Error("worker repository must not be called")
	}

	if repo.claimCalled {
		t.Error("task repository must not be called")
	}
}

func TestService_ClaimRejectsWorkerIDMismatch(t *testing.T) {
	contextWorkerID := uuid.New()
	inputWorkerID := uuid.New()

	repo := &taskRepositoryStub{}
	workersRepo := &workerRepositoryStub{}
	service := NewService(repo, workersRepo)

	ctx := worker.WithID(t.Context(), contextWorkerID)

	_, err := service.Claim(ctx, ClaimTaskInput{WorkerID: inputWorkerID})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidInput)
	}

	if workersRepo.getWorkerCapabilitiesCalled {
		t.Error("worker repository must not be called")
	}

	if repo.claimCalled {
		t.Error("task repository must not be called")
	}
}

func TestService_ClaimPropagatesWorkerRepositoryError(t *testing.T) {
	workerID := uuid.New()
	repositoryErr := errors.New("database unavailable")

	repo := &taskRepositoryStub{}
	workersRepo := &workerRepositoryStub{
		getWorkerCapabilitiesErr: repositoryErr,
	}
	service := NewService(repo, workersRepo)

	ctx := worker.WithID(t.Context(), workerID)

	_, err := service.Claim(ctx, ClaimTaskInput{WorkerID: workerID})
	if !errors.Is(err, repositoryErr) {
		t.Fatalf("error = %v, want wrapped %v", err, repositoryErr)
	}

	if repo.claimCalled {
		t.Error("task repository must not be called after worker repository failure")
	}
}

func TestService_ClaimPropagatesNoAvailableTasks(t *testing.T) {
	workerID := uuid.New()

	repo := &taskRepositoryStub{
		claimErr: ErrNoAvailableTasks,
	}
	workersRepo := &workerRepositoryStub{
		workerCapabilities: []string{"email-send"},
	}
	service := NewService(repo, workersRepo)

	ctx := worker.WithID(t.Context(), workerID)

	_, err := service.Claim(ctx, ClaimTaskInput{WorkerID: workerID})
	if !errors.Is(err, ErrNoAvailableTasks) {
		t.Fatalf("error = %v, want %v", err, ErrNoAvailableTasks)
	}
}

func TestService_ClaimPropagatesRepositoryError(t *testing.T) {
	workerID := uuid.New()
	repositoryErr := errors.New("claim query failed")

	repo := &taskRepositoryStub{claimErr: repositoryErr}
	workersRepo := &workerRepositoryStub{
		workerCapabilities: []string{"email-send"},
	}
	service := NewService(repo, workersRepo)

	ctx := worker.WithID(t.Context(), workerID)

	_, err := service.Claim(ctx, ClaimTaskInput{WorkerID: workerID})
	if !errors.Is(err, repositoryErr) {
		t.Fatalf("error = %v, want wrapped %v", err, repositoryErr)
	}
}
