package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const (
	ErrorCodeTaskTimeout        = "task_timeout"
	ErrorCodeTaskInvalidParam   = "task_invalid_param"
	ErrorCodeTaskInvalidPayload = "task_invalid_payload"
)

var ErrTaskValidationFailed = errors.New("task validation failed")

type ForgeTaskHandler interface {
	HandleTask(ctx context.Context, task Task) (ServiceResult, error)
}

type Service struct {
	handlers map[string]ForgeTaskHandler
}

func NewService(handlers map[string]ForgeTaskHandler) *Service {
	return &Service{handlers: handlers}
}

func (s *Service) Process(ctx context.Context, task Task) (ServiceResult, error) {
	if err := s.validateTask(task); err != nil {
		return ServiceResult{Error: err}, nil
	}

	if err := ctx.Err(); err != nil {
		return ServiceResult{}, err
	}

	handler, err := s.getTaskHandlerByType(task.Type)
	if err != nil {
		return ServiceResult{}, err
	}

	result, handlerErr := handler.HandleTask(ctx, task)

	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return ServiceResult{Error: &ServiceError{Code: ErrorCodeTaskTimeout, Message: "Task execution timed out", Retryable: true}}, nil
		}

		return ServiceResult{}, ctxErr
	}

	if handlerErr != nil {
		return ServiceResult{}, fmt.Errorf("handle task: %w", handlerErr)
	}

	return result, nil
}

func (s *Service) validateTask(task Task) *ServiceError {
	if task.ID == uuid.Nil {
		return &ServiceError{
			Code:      ErrorCodeTaskInvalidParam,
			Message:   "Task nil id provided",
			Retryable: false,
		}
	}

	if task.Timeout <= 0 {
		return &ServiceError{
			Code:      ErrorCodeTaskInvalidParam,
			Message:   "Task timeout zero or less",
			Retryable: false,
		}
	}

	if task.AttemptNumber <= 0 {
		return &ServiceError{
			Code:      ErrorCodeTaskInvalidParam,
			Message:   "Task attempt number zero or less",
			Retryable: false,
		}
	}

	return nil
}

func (s *Service) ValidateCapabilities(capabilities []string) error {
	for _, capability := range capabilities {
		if _, ok := s.handlers[capability]; !ok {
			return fmt.Errorf("capability %q has no registered task handler", capability)
		}
	}

	return nil
}

func (s *Service) getTaskHandlerByType(taskType string) (ForgeTaskHandler, error) {
	taskType = strings.TrimSpace(taskType)

	if taskType == "" {
		return nil, fmt.Errorf("task type is empty string: %w", ErrTaskValidationFailed)
	}

	handler, ok := s.handlers[taskType]
	if !ok {
		return nil, fmt.Errorf("task type is not supported: %w", ErrTaskValidationFailed)
	}

	return handler, nil
}
