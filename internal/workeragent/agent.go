package workeragent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"forge/internal/workeragent/forgeapi"
	"forge/internal/workeragent/task"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
)

const Protocol = "1"
const ClaimTaskPeriodSeconds = 10

var ErrWorkerIDConflict = errors.New("worker id conflict")
var ErrUnauthorized = errors.New("worker unauthorized")

type UnauthorizedError struct {
	Token string
}

type cycleResult struct {
	name string
	task *forgeapi.ClaimedTask
	err  error
}

type taskProcessResult struct {
	result task.ServiceResult
	err    error
}

const (
	cycleHeartbeat = "heartbeat"
	cycleClaimTask = "claim-task"
)

func (e *UnauthorizedError) Error() string {
	return ErrUnauthorized.Error()
}

func (e *UnauthorizedError) Unwrap() error {
	return ErrUnauthorized
}

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
	tokenMu               sync.RWMutex
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
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := a.registerAtForge(runCtx); err != nil {
		if runCtx.Err() != nil {
			return nil
		}
		return err
	}

	cycleResults := make(chan cycleResult, 2)

	startHeartbeat := func() {
		go func() {
			cycleResults <- cycleResult{name: cycleHeartbeat, err: a.startHeartbeatCycle(runCtx)}
		}()
	}

	startClaimTask := func() {
		go func() {
			claimedTask, err := a.startClaimTaskCycle(runCtx)
			cycleResults <- cycleResult{name: cycleClaimTask, task: claimedTask, err: err}
		}()
	}

	startHeartbeat()
	startClaimTask()

	taskResults := make(chan taskProcessResult, 1)

	for {
		select {
		case <-runCtx.Done():
			return nil

		case result := <-cycleResults:
			if runCtx.Err() != nil {
				return nil
			}

			if errors.Is(result.err, ErrUnauthorized) {
				if err := a.handleCycleAuthErr(runCtx, result.err); err != nil {
					if runCtx.Err() != nil {
						return nil
					}
					return err
				}

				switch result.name {
				case cycleHeartbeat:
					startHeartbeat()
				case cycleClaimTask:
					startClaimTask()
				default:
					return fmt.Errorf("unknown cycle: %s", result.name)
				}

				continue
			}

			if result.err != nil {
				return fmt.Errorf("%s cycle: %w", result.name, result.err)
			}

			if result.task != nil {
				go func() {
					taskResults <- a.processTask(runCtx, result.task)
				}()
			}

		case processed := <-taskResults:
			if err := a.handleTaskResult(runCtx, processed); err != nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}

				return err
			}

			startClaimTask()
		}
	}

}

func (a *Agent) processTask(ctx context.Context, claimedTask *forgeapi.ClaimedTask) taskProcessResult {
	a.taskID = claimedTask.ID
	a.leaseToken = claimedTask.LeaseToken
	a.leaseExpiresAt = claimedTask.LeaseExpiresAt

	taskForService := toTask(*claimedTask)

	taskCtx, cancel := context.WithTimeout(ctx, taskForService.Timeout)
	defer cancel()

	serviceResult, err := a.taskService.Process(taskCtx, taskForService)
	return taskProcessResult{result: serviceResult, err: err}
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
	a.setWorkerToken(resp.WorkerToken)

	return nil
}

func (a *Agent) registerWithRetry(ctx context.Context) error {
	var lastErr error

	for attempt := 1; attempt <= 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := a.registerAtForge(ctx); err != nil {
			lastErr = err
			continue
		}

		return nil
	}

	return fmt.Errorf("worker re-registration failed after 3 attempts: %w", lastErr)
}

func (a *Agent) startHeartbeatCycle(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			workerToken := a.getWorkerToken()
			resp, err := a.client.Heartbeat(ctx, workerToken, forgeapi.HeartbeatRequest{WorkerID: a.workerID})

			if err != nil {
				if ctx.Err() != nil {
					return nil
				}

				if isForgeAuthFailedError(err) {
					return &UnauthorizedError{Token: workerToken}
				}

				if isRetryableForgeError(err) {
					continue
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
	workerToken := a.getWorkerToken()
	claimedTask, err := a.claimTaskRequest(ctx, workerToken)
	if claimedTask != nil {
		return claimedTask, nil
	}

	if err != nil {
		if ctx.Err() != nil {
			return nil, nil
		}

		if isForgeAuthFailedError(err) {
			return nil, &UnauthorizedError{Token: workerToken}
		}

		if !isRetryableForgeError(err) {
			return nil, fmt.Errorf("claim task: %w", err)
		}
	}

	ticker := time.NewTicker(ClaimTaskPeriodSeconds * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, nil
		case <-ticker.C:
			workerToken := a.getWorkerToken()
			claimedTask, err := a.claimTaskRequest(ctx, workerToken)
			if claimedTask != nil {
				return claimedTask, nil
			}
			if err != nil {
				if ctx.Err() != nil {
					return nil, nil
				}

				if isForgeAuthFailedError(err) {
					return nil, &UnauthorizedError{Token: workerToken}
				}

				if isRetryableForgeError(err) {
					continue
				}

				return nil, fmt.Errorf("claim task: %w", err)
			}
		}
	}
}

func (a *Agent) claimTaskRequest(ctx context.Context, workerToken string) (*forgeapi.ClaimedTask, error) {
	resp, err := a.client.ClaimTask(ctx, workerToken, forgeapi.ClaimTaskRequest{WorkerID: a.workerID})
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
	resp, err := a.client.RenewTaskLease(ctx, a.getWorkerToken(), a.taskID, req)

	if err != nil {
		return err
	}

	a.leaseExpiresAt = resp.LeaseExpiresAt
	return nil
}

func (a *Agent) notifyTaskFail(ctx context.Context, code, message string, retryable bool) error {
	req := forgeapi.FailTaskRequest{
		WorkerID:   a.workerID,
		LeaseToken: a.leaseToken,
		Error:      forgeapi.TaskError{Code: code, Message: message},
		Retryable:  retryable,
	}

	err := a.client.FailTask(ctx, a.getWorkerToken(), a.taskID, req)

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

	if err := a.client.CompleteTask(ctx, a.getWorkerToken(), a.taskID, req); err != nil {
		return fmt.Errorf("notify about task complete: %w", err)
	}

	return nil
}

func (a *Agent) handleCycleAuthErr(ctx context.Context, resultErr error) error {
	var authErr *UnauthorizedError
	if !errors.As(resultErr, &authErr) {
		return fmt.Errorf("invalid authorization error: %w", resultErr)
	}

	// Проверяем, не обновил ли другой цикл токен.
	if authErr.Token == a.getWorkerToken() {
		if err := a.registerWithRetry(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
	}

	return nil
}

func (a *Agent) handleTaskResult(ctx context.Context, processed taskProcessResult) error {
	if ctx.Err() != nil {
		return nil
	}

	if processed.err != nil {
		if !errors.Is(processed.err, context.Canceled) {
			a.notifyForgeAboutInternalError(ctx, processed.err)
		}

		if ctx.Err() != nil {
			return nil
		}

		return fmt.Errorf("process task: %w", processed.err)
	}

	if processed.result.Error != nil {
		if err := a.notifyTaskFail(ctx, processed.result.Error.Code,
			processed.result.Error.Message, processed.result.Error.Retryable); err != nil {
			return err
		}
	} else {
		if err := a.notifyTaskComplete(ctx, processed.result); err != nil {
			return err
		}
	}

	return nil
}

func (a *Agent) notifyForgeAboutInternalError(ctx context.Context, cause error) {
	retryable := !errors.Is(cause, task.ErrTaskValidationFailed)

	if err := a.notifyTaskFail(ctx, "internal_err", "Worker failed to process task", retryable); err != nil && ctx.Err() == nil {
		log.Printf("failed to notify forge about task internal error: %v", err)
	}
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

func isRetryableForgeError(err error) bool {
	var remoteErr *forgeapi.RemoteError
	if errors.As(err, &remoteErr) {
		return remoteErr.StatusCode == http.StatusInternalServerError
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}

	return false
}

func isForgeAuthFailedError(err error) bool {
	var remoteErr *forgeapi.RemoteError
	if errors.As(err, &remoteErr) {
		return remoteErr.StatusCode == http.StatusUnauthorized
	}
	return false
}

func (a *Agent) getWorkerToken() string {
	a.tokenMu.RLock()
	defer a.tokenMu.RUnlock()

	return a.workerToken
}

func (a *Agent) setWorkerToken(token string) {
	a.tokenMu.Lock()
	defer a.tokenMu.Unlock()

	a.workerToken = token
}
