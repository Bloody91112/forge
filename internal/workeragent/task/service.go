package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
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
		return ServiceResult{}, err
	}

	handler, getHandlerErr := s.getTaskHandlerByType(task.Type)
	if getHandlerErr != nil {
		return ServiceResult{}, getHandlerErr
	}

	return handler.HandleTask(ctx, task)
}

func (s *Service) validateTask(task Task) error {
	if task.ID == uuid.Nil {
		return fmt.Errorf("task nil uuid provided: %w", ErrTaskValidationFailed)
	}

	if task.Timeout <= 0 {
		return fmt.Errorf("task timeout is zero or less: %w", ErrTaskValidationFailed)
	}

	if task.AttemptNumber <= 0 {
		return fmt.Errorf("attempt number is zero or less: %w", ErrTaskValidationFailed)
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
