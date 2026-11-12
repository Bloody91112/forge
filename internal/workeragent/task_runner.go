package workeragent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"forge/internal/workeragent/forgeapi"
	"forge/internal/workeragent/task"
)

const maxTaskResultSize = 1 << 20 // 1 MiB, as defined by the Forge v1 contract.

var errTaskLeaseLost = errors.New("task lease lost")

type taskProcessResult struct {
	claimedTask forgeapi.ClaimedTask
	result      task.ServiceResult
	err         error
}

func (a *Agent) waitForTask(ctx context.Context) (*forgeapi.ClaimedTask, error) {
	ticker := time.NewTicker(a.intervals.claim)
	defer ticker.Stop()

	for {
		workerToken := a.getWorkerToken()
		response, err := a.client.ClaimTask(ctx, workerToken, forgeapi.ClaimTaskRequest{WorkerID: a.workerID})
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if isForgeAuthFailedError(err) {
			if refreshErr := a.refreshWorkerToken(ctx, workerToken); refreshErr != nil {
				return nil, fmt.Errorf("refresh worker authorization after claim: %w", refreshErr)
			}
			continue
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
	leaseCtx, cancelLease := context.WithCancelCause(ctx)
	defer cancelLease(context.Canceled)

	taskCtx, cancelTask := context.WithTimeout(
		leaseCtx,
		time.Duration(claimedTask.TimeoutSeconds)*time.Second,
	)
	defer cancelTask()

	processDone := make(chan struct{})
	renewDone := make(chan error, 1)

	go func() {
		renewDone <- a.runRenewCycle(
			leaseCtx,
			taskCtx,
			processDone,
			cancelLease,
			claimedTask,
		)
	}()

	processed := a.processTask(taskCtx, claimedTask)

	close(processDone)
	cancelTask()

	if cause := context.Cause(leaseCtx); cause != nil {
		<-renewDone

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if errors.Is(cause, errTaskLeaseLost) {
			return nil
		}

		return fmt.Errorf("renew task lease: %w", cause)
	}

	resultErr := a.handleTaskResult(leaseCtx, processed)

	cancelLease(context.Canceled)

	renewErr := <-renewDone

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if errors.Is(context.Cause(leaseCtx), errTaskLeaseLost) {
		return nil
	}

	if renewErr != nil && !errors.Is(renewErr, errTaskLeaseLost) {
		return fmt.Errorf("renew task lease: %w", renewErr)
	}

	return resultErr
}

func (a *Agent) processTask(ctx context.Context, claimedTask forgeapi.ClaimedTask) taskProcessResult {
	taskForService := toTask(claimedTask)
	result, err := a.taskService.Process(ctx, taskForService)
	return taskProcessResult{
		claimedTask: claimedTask,
		result:      result,
		err:         err,
	}
}

func (a *Agent) runRenewCycle(
	ctx context.Context,
	taskCtx context.Context,
	processDone <-chan struct{},
	cancelLease context.CancelCauseFunc,
	claimedTask forgeapi.ClaimedTask,
) error {
	ticker := time.NewTicker(a.intervals.renew)
	defer ticker.Stop()

	taskDone := taskCtx.Done()
	processFinished := processDone

	stopOnTaskDeadline := func() bool {
		if taskDone == nil {
			return false
		}

		select {
		case <-taskDone:
			select {
			case <-processDone:
				taskDone = nil
				processFinished = nil
				return false
			default:
				return true
			}
		default:
			return false
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-processFinished:
			processFinished = nil
			taskDone = nil

		case <-taskDone:
			if stopOnTaskDeadline() {
				return nil
			}

		case <-ticker.C:
			if stopOnTaskDeadline() {
				return nil
			}

			workerToken := a.getWorkerToken()

			request := forgeapi.RenewTaskRequest{
				WorkerID:   a.workerID,
				LeaseToken: claimedTask.LeaseToken,
			}

			_, err := a.client.RenewTaskLease(ctx, workerToken, claimedTask.ID, request)

			if ctx.Err() != nil {
				return nil
			}

			if isForgeAuthFailedError(err) {
				if refreshErr := a.refreshWorkerToken(ctx, workerToken); refreshErr != nil {
					if ctx.Err() != nil {
						return nil
					}

					refreshErr = fmt.Errorf(
						"refresh worker authorization after lease renewal: %w",
						refreshErr,
					)

					cancelLease(refreshErr)
					return refreshErr
				}

				continue
			}

			if isTaskLeaseLostError(err) {
				cancelLease(errTaskLeaseLost)
				return errTaskLeaseLost
			}

			if err != nil {
				if !isRetryableForgeError(err) {
					cancelLease(err)
					return err
				}

				log.Printf("renew task lease failed; retrying: %v", err)
			}
		}
	}
}

func (a *Agent) handleTaskResult(ctx context.Context, processed taskProcessResult) error {
	if processed.err != nil {
		if err := a.notifyForgeAboutInternalError(ctx, processed.claimedTask, processed.err); err != nil {
			if isTaskLeaseLostError(err) {
				return nil
			}
			return fmt.Errorf("report task processing error: %w", err)
		}
		return nil
	}

	if resultErr := processed.result.Error; resultErr != nil {
		if err := a.notifyTaskFail(ctx, processed.claimedTask, resultErr.Code, resultErr.Message, resultErr.Retryable); err != nil {
			if isTaskLeaseLostError(err) {
				return nil
			}
			return err
		}
		return nil
	}

	result, err := marshalTaskResult(processed.result.Result)
	if err != nil {
		if notifyErr := a.notifyTaskFail(ctx, processed.claimedTask, "invalid_task_result", err.Error(), false); notifyErr != nil {
			if isTaskLeaseLostError(notifyErr) {
				return nil
			}
			return notifyErr
		}
		return nil
	}

	if err := a.notifyTaskComplete(ctx, processed.claimedTask, result); err != nil {
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

func isTaskLeaseLostError(err error) bool {
	var remoteErr *forgeapi.RemoteError
	return errors.As(err, &remoteErr) &&
		remoteErr.StatusCode == http.StatusConflict &&
		remoteErr.Code == "lease_lost"
}
