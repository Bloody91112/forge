package workeragent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"forge/internal/workeragent/forgeapi"
	"forge/internal/workeragent/task"
	"time"

	"github.com/google/uuid"
)

const Protocol = "1"
const ClaimTaskPeriodSeconds = 10

var ErrWorkerIDConflict = errors.New("worker id conflict")

type ForgeClient interface {
	Register(ctx context.Context, bootstrapToken string, input forgeapi.RegisterRequest) (forgeapi.RegisterResponse, error)
	Heartbeat(ctx context.Context, workerToken string, input forgeapi.HeartbeatRequest) (forgeapi.HeartbeatResponse, error)
	ClaimTask(ctx context.Context, workerToken string, input forgeapi.ClaimTaskRequest) (*forgeapi.ClaimTaskResponse, error)
	RenewTaskLease(ctx context.Context, workerToken string, taskID uuid.UUID, input forgeapi.RenewTaskRequest) (forgeapi.RenewTaskResponse, error)
	CompleteTask(ctx context.Context, workerToken string, taskID uuid.UUID, input forgeapi.CompleteTaskRequest) error
	FailTask(ctx context.Context, workerToken string, taskID uuid.UUID, input forgeapi.FailTaskRequest) error
}

type TaskService interface {
	Process(ctx context.Context, task task.Task) (task.ServiceResult, error)
}

type Agent struct {
	// input
	client         ForgeClient
	taskService    TaskService
	bootstrapToken string
	capabilities   []string
	// initial
	workerID              uuid.UUID
	workerToken           string
	registrationExpiresAt time.Time

	// task
	taskID         uuid.UUID
	leaseToken     string
	leaseExpiresAt time.Time
}

func NewAgent(client ForgeClient, taskService TaskService, bootstrapToken string, capabilities []string) *Agent {
	return &Agent{
		client:         client,
		taskService:    taskService,
		bootstrapToken: bootstrapToken,
		capabilities:   capabilities,
		workerID:       uuid.New(),
	}
}

func (a *Agent) Run(ctx context.Context) error {
	if err := a.registerAtForge(ctx); err != nil {
		return err
	}

	return nil
}

func (a *Agent) registerAtForge(ctx context.Context) error {
	registerRequest := forgeapi.RegisterRequest{
		WorkerID:        a.workerID,
		ProtocolVersion: Protocol,
		Capabilities:    a.capabilities,
	}

	resp, err := a.client.Register(ctx, a.bootstrapToken, registerRequest)

	if err != nil {
		return fmt.Errorf("register worker: %w", err)
	}

	if resp.WorkerID != a.workerID {
		return ErrWorkerIDConflict
	}

	a.registrationExpiresAt = resp.LeaseExpiresAt
	a.workerToken = resp.WorkerToken

	return nil
}

func (a *Agent) startHeartbeatCycle(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			resp, err := a.client.Heartbeat(ctx, a.workerToken, forgeapi.HeartbeatRequest{WorkerID: a.workerID})
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}

				return fmt.Errorf("heartbeat worker: %w", err)
			}

			if resp.WorkerID != a.workerID {
				return ErrWorkerIDConflict
			}

			a.registrationExpiresAt = resp.LeaseExpiresAt
		}
	}
}

func (a *Agent) startClaimTaskCycle(ctx context.Context) (*forgeapi.ClaimedTask, error) {
	if claimedTask, err := a.claimTaskRequest(ctx); err != nil || claimedTask != nil {
		return claimedTask, err
	}

	ticker := time.NewTicker(ClaimTaskPeriodSeconds * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, nil
		case <-ticker.C:
			if claimedTask, err := a.claimTaskRequest(ctx); err != nil || claimedTask != nil {
				return claimedTask, err
			}
		}
	}
}

func (a *Agent) claimTaskRequest(ctx context.Context) (*forgeapi.ClaimedTask, error) {
	resp, err := a.client.ClaimTask(ctx, a.workerToken, forgeapi.ClaimTaskRequest{WorkerID: a.workerID})
	if err != nil {
		return nil, fmt.Errorf("claim task: %w", err)
	}

	if resp != nil {
		return &resp.Task, nil
	}

	return nil, nil
}

func (a *Agent) renewTaskLease(ctx context.Context) error {
	req := forgeapi.RenewTaskRequest{WorkerID: a.workerID, LeaseToken: a.leaseToken}
	resp, err := a.client.RenewTaskLease(ctx, a.workerToken, a.taskID, req)

	if err != nil {
		return err
	}

	a.leaseExpiresAt = resp.LeaseExpiresAt
	return nil
}

func (a *Agent) notifyTaskFail(ctx context.Context, taskError task.ServiceError) error {
	req := forgeapi.FailTaskRequest{
		WorkerID:   a.workerID,
		LeaseToken: a.leaseToken,
		Error:      forgeapi.TaskError{Code: taskError.Code, Message: taskError.Message},
		Retryable:  taskError.Retryable,
	}

	err := a.client.FailTask(ctx, a.workerToken, a.taskID, req)

	if err != nil {
		return fmt.Errorf("notify about task fail: %w", err)
	}

	return nil
}

func (a *Agent) notifyTaskComplete(ctx context.Context, taskResult task.ServiceResult) error {
	result, err := json.Marshal(taskResult.Result)
	if err != nil {
		return fmt.Errorf("marshal task result: %w", err)
	}

	if len(result) == 0 || result[0] != '{' {
		return errors.New("task result must be a JSON object")
	}

	req := forgeapi.CompleteTaskRequest{
		WorkerID:   a.workerID,
		LeaseToken: a.leaseToken,
		Result:     json.RawMessage(result),
	}

	if err := a.client.CompleteTask(ctx, a.workerToken, a.taskID, req); err != nil {
		return fmt.Errorf("notify about task complete: %w", err)
	}

	return nil
}

func toTask(claimed forgeapi.ClaimedTask) task.Task {
	return task.Task{
		ID:            claimed.ID,
		Type:          claimed.Type,
		Payload:       claimed.Payload,
		AttemptNumber: claimed.AttemptNumber,
		Timeout:       time.Duration(claimed.TimeoutSeconds) * time.Second,
	}
}
