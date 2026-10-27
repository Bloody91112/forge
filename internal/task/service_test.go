package task

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
)

type TaskRepositoryStub struct {
	task Task

	lookupTask    Task
	lookupTaskSet bool
	lookupErr     error
	lookupCalled  bool
	lookupClient  uuid.UUID
	lookupKey     string

	getByIDErr    error
	getByIDCalled bool
	gotTaskID     uuid.UUID
	gotClientID   uuid.UUID

	createTask    Task
	createTaskSet bool
	createCreated bool
	createErr     error
	createCalled  bool
	createInput   Task
}

func (r *TaskRepositoryStub) CreateOrGetByIdempotencyKey(
	_ context.Context,
	task Task,
) (Task, bool, error) {
	r.createCalled = true
	r.createInput = task

	if r.createErr != nil {
		return Task{}, false, r.createErr
	}

	if r.createTaskSet {
		return r.createTask, r.createCreated, nil
	}

	return task, true, nil
}

func (r *TaskRepositoryStub) GetByID(
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

func (r *TaskRepositoryStub) GetByIdempotencyKey(
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

	if r.lookupTaskSet {
		return r.lookupTask, nil
	}

	return Task{}, ErrTaskNotFound
}

type WorkerRepositoryStub struct {
	capabilities [][]string
	err          error
	called       bool
}

func (r *WorkerRepositoryStub) GetActiveWorkersCapabilities(
	_ context.Context,
) ([][]string, error) {
	r.called = true

	if r.err != nil {
		return nil, r.err
	}

	return r.capabilities, nil
}

func newCompatibleWorkerRepository(taskType string) *WorkerRepositoryStub {
	return &WorkerRepositoryStub{
		capabilities: [][]string{
			{taskType},
		},
	}
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

func validCreateContext() context.Context {
	return WithClientID(context.Background(), uuid.New())
}

func TestService_GetByID_TaskExists(t *testing.T) {
	taskID := uuid.New()
	clientID := uuid.New()

	repo := &TaskRepositoryStub{
		task: Task{ID: taskID},
	}
	service := NewService(repo, &WorkerRepositoryStub{})
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
	repo := &TaskRepositoryStub{
		getByIDErr: ErrTaskNotFound,
	}
	service := NewService(repo, &WorkerRepositoryStub{})
	ctx := WithClientID(t.Context(), uuid.New())

	_, err := service.GetByID(ctx, uuid.New())
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("error = %v, want %v", err, ErrTaskNotFound)
	}
}

func TestService_GetByID_WithoutClientID(t *testing.T) {
	repo := &TaskRepositoryStub{}
	service := NewService(repo, &WorkerRepositoryStub{})

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

	repo := &TaskRepositoryStub{}
	workersRepo := newCompatibleWorkerRepository(input.Type)
	service := NewService(repo, workersRepo)

	got, err := service.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	hash, err := calculateRequestHash(
		input.Payload,
		input.Type,
		*input.TimeoutSeconds,
		*input.MaxAttempts,
	)
	if err != nil {
		t.Fatalf("calculateRequestHash() error = %v", err)
	}

	want := Task{
		ClientID:       clientID,
		IdempotencyKey: input.IdempotencyKey,
		RequestHash:    hash,
		Type:           input.Type,
		Payload:        input.Payload,
		Status:         StatusQueued,
		MaxAttempts:    *input.MaxAttempts,
		TimeoutSeconds: *input.TimeoutSeconds,
	}

	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(Task{}, "ID")); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
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
	repo := &TaskRepositoryStub{}
	workersRepo := newCompatibleWorkerRepository(input.Type)
	service := NewService(repo, workersRepo)

	got, err := service.Create(WithClientID(t.Context(), clientID), input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	hash, err := calculateRequestHash(
		input.Payload,
		input.Type,
		DefaultTaskTimeout,
		DefaultTaskMaxAttempts,
	)
	if err != nil {
		t.Fatalf("calculateRequestHash() error = %v", err)
	}

	want := Task{
		ClientID:       clientID,
		IdempotencyKey: input.IdempotencyKey,
		RequestHash:    hash,
		Type:           input.Type,
		Payload:        input.Payload,
		Status:         StatusQueued,
		MaxAttempts:    DefaultTaskMaxAttempts,
		TimeoutSeconds: DefaultTaskTimeout,
	}

	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(Task{}, "ID")); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}
}

func TestService_CreateExistingIdempotencyKeyReturnsTaskWithoutCheckingWorkers(t *testing.T) {
	input := validCreateTaskInput()
	clientID := uuid.New()

	hash, err := calculateRequestHash(
		input.Payload,
		input.Type,
		*input.TimeoutSeconds,
		*input.MaxAttempts,
	)
	if err != nil {
		t.Fatalf("calculateRequestHash() error = %v", err)
	}

	existingTask := Task{
		ID:             uuid.New(),
		ClientID:       clientID,
		IdempotencyKey: input.IdempotencyKey,
		RequestHash:    hash,
		Type:           input.Type,
		Payload:        input.Payload,
		Status:         StatusQueued,
		MaxAttempts:    *input.MaxAttempts,
		TimeoutSeconds: *input.TimeoutSeconds,
	}

	repo := &TaskRepositoryStub{
		lookupTask:    existingTask,
		lookupTaskSet: true,
	}
	workersRepo := &WorkerRepositoryStub{}
	service := NewService(repo, workersRepo)

	got, err := service.Create(WithClientID(t.Context(), clientID), input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if diff := cmp.Diff(existingTask, got); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
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

	existingTask := Task{
		ID:             uuid.New(),
		ClientID:       clientID,
		IdempotencyKey: input.IdempotencyKey,
		RequestHash:    "different-hash",
	}

	repo := &TaskRepositoryStub{
		lookupTask:    existingTask,
		lookupTaskSet: true,
	}
	workersRepo := &WorkerRepositoryStub{}
	service := NewService(repo, workersRepo)

	got, err := service.Create(WithClientID(t.Context(), clientID), input)
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("error = %v, want %v", err, ErrIdempotencyConflict)
	}

	if diff := cmp.Diff(existingTask, got); diff != "" {
		t.Errorf("returned task mismatch (-want +got):\n%s", diff)
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

	hash, err := calculateRequestHash(
		input.Payload,
		input.Type,
		*input.TimeoutSeconds,
		*input.MaxAttempts,
	)
	if err != nil {
		t.Fatalf("calculateRequestHash() error = %v", err)
	}

	existingTask := Task{
		ID:             uuid.New(),
		ClientID:       clientID,
		IdempotencyKey: input.IdempotencyKey,
		RequestHash:    hash,
		Type:           input.Type,
		Payload:        input.Payload,
		Status:         StatusQueued,
		MaxAttempts:    *input.MaxAttempts,
		TimeoutSeconds: *input.TimeoutSeconds,
	}

	repo := &TaskRepositoryStub{
		createTask:    existingTask,
		createTaskSet: true,
		createCreated: false,
	}
	workersRepo := newCompatibleWorkerRepository(input.Type)
	service := NewService(repo, workersRepo)

	got, err := service.Create(WithClientID(t.Context(), clientID), input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if diff := cmp.Diff(existingTask, got); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}

	if !repo.createCalled {
		t.Fatal("expected CreateOrGetByIdempotencyKey to be called")
	}
}

func TestService_CreateWithConcurrentDifferentRequestReturnsConflict(t *testing.T) {
	input := validCreateTaskInput()
	clientID := uuid.New()

	existingTask := Task{
		ID:             uuid.New(),
		ClientID:       clientID,
		IdempotencyKey: input.IdempotencyKey,
		RequestHash:    "different-hash",
	}

	repo := &TaskRepositoryStub{
		createTask:    existingTask,
		createTaskSet: true,
		createCreated: false,
	}
	workersRepo := newCompatibleWorkerRepository(input.Type)
	service := NewService(repo, workersRepo)

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
	repo := &TaskRepositoryStub{}
	workersRepo := &WorkerRepositoryStub{}
	service := NewService(repo, workersRepo)

	_, err := service.Create(validCreateContext(), input)
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
	repo := &TaskRepositoryStub{}
	workersRepo := &WorkerRepositoryStub{
		capabilities: [][]string{
			{input.Type, "image-resize"},
			{"report-generate"},
		},
	}
	service := NewService(repo, workersRepo)

	_, err := service.Create(validCreateContext(), input)
	if !errors.Is(err, ErrTaskTypeNotSupported) {
		t.Fatalf("error = %v, want %v", err, ErrTaskTypeNotSupported)
	}

	if repo.createCalled {
		t.Error("task must not be created when an active worker is incompatible")
	}
}

func TestService_CreateAcceptsTaskWhenAllActiveWorkersAreCompatible(t *testing.T) {
	input := validCreateTaskInput()
	repo := &TaskRepositoryStub{}
	workersRepo := &WorkerRepositoryStub{
		capabilities: [][]string{
			{input.Type},
			{input.Type, "image-resize"},
			{"report-generate", input.Type},
		},
	}
	service := NewService(repo, workersRepo)

	got, err := service.Create(validCreateContext(), input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if got.Type != input.Type {
		t.Errorf("task type = %q, want %q", got.Type, input.Type)
	}

	if !repo.createCalled {
		t.Fatal("expected task repository to be called")
	}
}

func TestService_CreatePropagatesIdempotencyLookupError(t *testing.T) {
	input := validCreateTaskInput()
	lookupErr := errors.New("database unavailable")

	repo := &TaskRepositoryStub{
		lookupErr: lookupErr,
	}
	workersRepo := &WorkerRepositoryStub{}
	service := NewService(repo, workersRepo)

	_, err := service.Create(validCreateContext(), input)
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

	repo := &TaskRepositoryStub{}
	workersRepo := &WorkerRepositoryStub{err: workersErr}
	service := NewService(repo, workersRepo)

	_, err := service.Create(validCreateContext(), input)
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

	repo := &TaskRepositoryStub{createErr: createErr}
	workersRepo := newCompatibleWorkerRepository(input.Type)
	service := NewService(repo, workersRepo)

	_, err := service.Create(validCreateContext(), input)
	if !errors.Is(err, createErr) {
		t.Fatalf("error = %v, want wrapped %v", err, createErr)
	}
}

func TestService_Create_WithoutClientID(t *testing.T) {
	input := validCreateTaskInput()
	repo := &TaskRepositoryStub{}
	workersRepo := &WorkerRepositoryStub{}
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

			repo := &TaskRepositoryStub{}
			workersRepo := newCompatibleWorkerRepository(input.Type)
			service := NewService(repo, workersRepo)

			_, err := service.Create(validCreateContext(), input)

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
			repo := &TaskRepositoryStub{}
			workersRepo := &WorkerRepositoryStub{}
			service := NewService(repo, workersRepo)

			_, err := service.Create(validCreateContext(), tt.input())
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
