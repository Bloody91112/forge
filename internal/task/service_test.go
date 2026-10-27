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
	task          Task
	taskPassed    bool
	created       bool
	err           error
	gotTaskID     uuid.UUID
	gotClientID   uuid.UUID
	getByIDCalled bool
}

func (r *TaskRepositoryStub) CreateOrGetByIdempotencyKey(ctx context.Context, task Task) (Task, bool, error) {
	if r.taskPassed || !r.created {
		return r.task, r.created, r.err
	}

	return task, r.created, r.err
}

func (r *TaskRepositoryStub) GetByID(ctx context.Context, taskID uuid.UUID, clientID uuid.UUID) (Task, error) {
	r.getByIDCalled = true
	r.gotTaskID = taskID
	r.gotClientID = clientID

	return r.task, r.err
}

func TestService_GetByID_TaskExists(t *testing.T) {
	taskID := uuid.New()
	clientID := uuid.New()

	repo := TaskRepositoryStub{
		task: Task{ID: taskID},
		err:  nil,
	}

	service := NewService(&repo)
	ctx := WithClientID(t.Context(), clientID)
	task, err := service.GetByID(ctx, taskID)
	if err != nil {
		t.Fatalf("get user error: %v", err)
	}

	if task.ID != taskID {
		t.Fatalf("expected task id: %d, got: %d", taskID, task.ID)
	}

	if !repo.getByIDCalled {
		t.Fatal("expected repository GetByID to be called")
	}

	if repo.gotTaskID != taskID {
		t.Errorf("expected taskID %s, got %s", taskID, repo.gotTaskID)
	}

	if repo.gotClientID != clientID {
		t.Errorf("expected clientID %s, got %s", clientID, repo.gotClientID)
	}

}

func TestService_GetByID_TaskNotFound(t *testing.T) {
	notExistTaskID := uuid.New()
	clientID := uuid.New()

	repo := TaskRepositoryStub{
		err: ErrTaskNotFound,
	}

	service := NewService(&repo)
	ctx := WithClientID(t.Context(), clientID)

	_, err := service.GetByID(ctx, notExistTaskID)

	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected err: %v, got: %v", ErrTaskNotFound, err)
	}
}

func TestService_GetByID_ThrowsErrOnClientIDMiss(t *testing.T) {
	repo := TaskRepositoryStub{}
	service := NewService(&repo)

	_, err := service.GetByID(t.Context(), uuid.New())

	if !errors.Is(err, ErrMissingClientID) {
		t.Fatalf("expected err: %v, got: %v", ErrMissingClientID, err)
	}

	if repo.getByIDCalled {
		t.Fatal("expected repository GetByID not to be called")
	}
}

func TestService_Create(t *testing.T) {
	idempotencyKey := uuid.New().String()
	clientID := uuid.New()
	taskType := "email-send"
	taskPayload := []byte(`{"user_id":42}`)
	taskTimeout := int64(22)
	taskMaxAttempts := int64(3)

	hash, err := calculateRequestHash(taskPayload, taskType, taskTimeout, taskMaxAttempts)

	if err != nil {
		t.Fatalf("failed to make request hash: %v", err)
	}

	ctx := WithClientID(t.Context(), clientID)

	createTaskDTO := CreateTaskInput{
		Type:           taskType,
		Payload:        taskPayload,
		IdempotencyKey: idempotencyKey,
		MaxAttempts:    &taskMaxAttempts,
		TimeoutSeconds: &taskTimeout,
	}

	expected := Task{
		ClientID:       clientID,
		IdempotencyKey: idempotencyKey,
		RequestHash:    hash,
		Type:           taskType,
		Payload:        taskPayload,
		Status:         StatusQueued,
		MaxAttempts:    taskMaxAttempts,
		TimeoutSeconds: taskTimeout,
	}

	repo := TaskRepositoryStub{
		err:        nil,
		created:    true,
		taskPassed: false,
	}

	service := NewService(&repo)

	actual, err := service.Create(ctx, createTaskDTO)

	if err != nil {
		t.Fatalf("service failed to create task, err: %v", err)
	}

	if diff := cmp.Diff(expected, actual, cmpopts.IgnoreFields(Task{}, "ID")); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}

	repo = TaskRepositoryStub{
		err:        nil,
		created:    false,
		taskPassed: true,
		task:       actual,
	}

	service = NewService(&repo)
	taskFromSameRequest, err := service.Create(ctx, createTaskDTO)

	if err != nil {
		t.Fatalf("service failed to create task, err: %v", err)
	}

	if diff := cmp.Diff(actual, taskFromSameRequest); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}

}

func TestService_CreateWithDefaultFields(t *testing.T) {
	idempotencyKey := uuid.New().String()
	clientID := uuid.New()
	taskType := "email-send"
	taskPayload := []byte(`{"user_id":42}`)

	hash, err := calculateRequestHash(taskPayload, taskType, DefaultTaskTimeout, DefaultTaskMaxAttempts)

	if err != nil {
		t.Fatalf("failed to make request hash: %v", err)
	}

	ctx := WithClientID(t.Context(), clientID)

	createTaskDTO := CreateTaskInput{
		Type:           taskType,
		Payload:        taskPayload,
		IdempotencyKey: idempotencyKey,
	}

	expected := Task{
		ClientID:       clientID,
		IdempotencyKey: idempotencyKey,
		RequestHash:    hash,
		Type:           taskType,
		Payload:        taskPayload,
		Status:         StatusQueued,
		MaxAttempts:    DefaultTaskMaxAttempts,
		TimeoutSeconds: DefaultTaskTimeout,
	}

	repo := TaskRepositoryStub{
		err:        nil,
		taskPassed: false,
		created:    true,
	}

	service := NewService(&repo)

	actual, err := service.Create(ctx, createTaskDTO)

	if err != nil {
		t.Fatalf("service failed to create task, err: %v", err)
	}

	if diff := cmp.Diff(expected, actual, cmpopts.IgnoreFields(Task{}, "ID")); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}
}

func TestService_CreateWithConflictHash(t *testing.T) {
	idempotencyKey := uuid.New().String()
	clientID := uuid.New()
	taskType := "email-send"
	taskPayload := []byte(`{"user_id":42}`)
	taskTimeout := int64(22)
	taskMaxAttempts := int64(3)

	taskHash, err := calculateRequestHash(taskPayload, taskType, taskTimeout, taskMaxAttempts)

	if err != nil {
		t.Fatalf("failed to make request hash: %v", err)
	}

	ctx := WithClientID(t.Context(), clientID)

	// input task timeout changed, hash conflict
	otherTaskTimeout := int64(21)

	createTaskDTO := CreateTaskInput{
		Type:           taskType,
		Payload:        taskPayload,
		IdempotencyKey: idempotencyKey,
		MaxAttempts:    &taskMaxAttempts,
		TimeoutSeconds: &otherTaskTimeout,
	}

	expected := Task{
		RequestHash: taskHash,
	}

	repo := TaskRepositoryStub{
		err:        nil,
		created:    false,
		task:       expected,
		taskPassed: true,
	}

	service := NewService(&repo)

	_, err = service.Create(ctx, createTaskDTO)

	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected err: %v, got: %v", ErrIdempotencyConflict, err)
	}
}

func TestService_Create_ThrowsErrorOnClientIDMiss(t *testing.T) {
	createTaskDTO := CreateTaskInput{
		Type:           "email-send",
		Payload:        []byte(`{"user_id":42}`),
		IdempotencyKey: uuid.New().String(),
	}

	repo := TaskRepositoryStub{
		err:        nil,
		taskPassed: false,
		created:    true,
	}

	service := NewService(&repo)

	_, err := service.Create(t.Context(), createTaskDTO)

	if !errors.Is(err, ErrMissingClientID) {
		t.Fatalf("expected err: %v, got: %v", ErrMissingClientID, err)
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
			repo := &TaskRepositoryStub{
				created: true,
			}
			service := NewService(repo)

			maxAttempts := tt.maxAttempts
			timeoutSeconds := tt.timeoutSeconds

			ctx := WithClientID(t.Context(), uuid.New())

			_, err := service.Create(ctx, CreateTaskInput{
				Type:           "email-send",
				Payload:        []byte(`{"user_id":42}`),
				IdempotencyKey: uuid.NewString(),
				MaxAttempts:    &maxAttempts,
				TimeoutSeconds: &timeoutSeconds,
			})

			if tt.wantErr {
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf(
						"expected ErrInvalidInput, got %v",
						err,
					)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected success, got %v", err)
			}
		})
	}
}
