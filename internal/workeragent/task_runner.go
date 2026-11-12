package workeragent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"forge/internal/workeragent/forgeapi"
	"forge/internal/workeragent/task"
)

const maxTaskResultSize = 1 << 20 // 1 MiB, as defined by the Forge v1 contract.

var errTaskLeaseLost = errors.New("task lease lost")

func (a *Agent) waitForTask(ctx context.Context) (*forgeapi.ClaimedTask, error) {
	ticker := time.NewTicker(a.intervals.claim)
	defer ticker.Stop()

	for {
		var response *forgeapi.ClaimTaskResponse
		err := a.withWorkerAuthorizationRetry(ctx, func(workerToken string) error {
			var requestErr error
			response, requestErr = a.client.ClaimTask(
				ctx,
				workerToken,
				forgeapi.ClaimTaskRequest{WorkerID: a.workerID},
			)
			return requestErr
		})

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if isWorkerAuthorizationFailedError(err) {
			return nil, fmt.Errorf("worker authorization: %w", err)
		}

		if err != nil {
			if !isRetryableForgeError(err) {
				return nil, fmt.Errorf("claim task: %w", err)
			}
			log.Printf("claim task failed; retrying: %v", err)
		} else if response != nil {
			return &response.Task, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a *Agent) runTask(ctx context.Context, claimedTask forgeapi.ClaimedTask) error {
	taskCtx, cancelTask := context.WithTimeout(
		ctx,
		time.Duration(claimedTask.TimeoutSeconds)*time.Second,
	)
	defer cancelTask()

	renewDone := make(chan error, 1)
	go func() {
		renewDone <- a.runRenewCycle(taskCtx, cancelTask, claimedTask)
	}()

	result, processErr := a.taskService.Process(taskCtx, toTask(claimedTask))
	cancelTask()
	renewErr := <-renewDone

	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(renewErr, errTaskLeaseLost) {
		return nil
	}
	if renewErr != nil {
		return fmt.Errorf("renew task lease: %w", renewErr)
	}

	return a.handleTaskResult(ctx, claimedTask, result, processErr)
}

func (a *Agent) runRenewCycle(
	ctx context.Context,
	cancelTask context.CancelFunc,
	claimedTask forgeapi.ClaimedTask,
) error {
	ticker := time.NewTicker(a.intervals.renew)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-ticker.C:
			err := a.withWorkerAuthorizationRetry(ctx, func(workerToken string) error {
				request := forgeapi.RenewTaskRequest{
					WorkerID:   a.workerID,
					LeaseToken: claimedTask.LeaseToken,
				}
				_, requestErr := a.client.RenewTaskLease(ctx, workerToken, claimedTask.ID, request)
				return requestErr
			})

			if ctx.Err() != nil {
				return nil
			}
			if isTaskLeaseLostError(err) {
				cancelTask()
				return errTaskLeaseLost
			}
			if isWorkerAuthorizationFailedError(err) {
				cancelTask()
				return err
			}
			if err != nil {
				log.Printf("renew task lease failed; retrying: %v", err)
			}
		}
	}
}

func (a *Agent) handleTaskResult(
	ctx context.Context,
	claimedTask forgeapi.ClaimedTask,
	result task.ServiceResult,
	processErr error,
) error {
	if processErr != nil {
		if err := a.notifyForgeAboutInternalError(ctx, claimedTask, processErr); err != nil {
			if isTaskLeaseLostError(err) {
				return nil
			}
			return fmt.Errorf("report task processing error: %w", err)
		}
		return nil
	}

	if resultErr := result.Error; resultErr != nil {
		if err := a.notifyTaskFail(ctx, claimedTask, resultErr.Code, resultErr.Message, resultErr.Retryable); err != nil {
			if isTaskLeaseLostError(err) {
				return nil
			}
			return err
		}
		return nil
	}

	marshaledResult, err := marshalTaskResult(result.Result)
	if err != nil {
		if notifyErr := a.notifyTaskFail(ctx, claimedTask, "invalid_task_result", err.Error(), false); notifyErr != nil {
			if isTaskLeaseLostError(notifyErr) {
				return nil
			}
			return notifyErr
		}
		return nil
	}

	if err := a.notifyTaskComplete(ctx, claimedTask, marshaledResult); err != nil {
		if isTaskLeaseLostError(err) {
			return nil
		}
		return err
	}

	return nil
}

func (a *Agent) notifyForgeAboutInternalError(ctx context.Context, claimedTask forgeapi.ClaimedTask, cause error) error {
	retryable := !errors.Is(cause, task.ErrTaskValidationFailed)
	return a.notifyTaskFail(ctx, claimedTask, "internal_err", "Worker failed to process task", retryable)
}

func (a *Agent) notifyTaskFail(ctx context.Context, claimedTask forgeapi.ClaimedTask, code, message string, retryable bool) error {
	request := forgeapi.FailTaskRequest{
		WorkerID:   a.workerID,
		LeaseToken: claimedTask.LeaseToken,
		Error:      forgeapi.TaskError{Code: code, Message: message},
		Retryable:  retryable,
	}

	err := a.withWorkerAuthorizationRetry(ctx, func(workerToken string) error {
		return a.client.FailTask(ctx, workerToken, claimedTask.ID, request)
	})
	if err != nil {
		return fmt.Errorf("notify about task failure: %w", err)
	}
	return nil
}

func (a *Agent) notifyTaskComplete(ctx context.Context, claimedTask forgeapi.ClaimedTask, result json.RawMessage) error {
	request := forgeapi.CompleteTaskRequest{
		WorkerID:   a.workerID,
		LeaseToken: claimedTask.LeaseToken,
		Result:     result,
	}

	err := a.withWorkerAuthorizationRetry(ctx, func(workerToken string) error {
		return a.client.CompleteTask(ctx, workerToken, claimedTask.ID, request)
	})
	if err != nil {
		return fmt.Errorf("notify about task completion: %w", err)
	}
	return nil
}

func marshalTaskResult(value any) (json.RawMessage, error) {
	result, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal task result: %w", err)
	}
	if len(result) == 0 || result[0] != '{' {
		return nil, errors.New("task result must be a JSON object")
	}
	if len(result) > maxTaskResultSize {
		return nil, fmt.Errorf("task result exceeds %d bytes", maxTaskResultSize)
	}
	return json.RawMessage(result), nil
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
