package task

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
)

var supportedTaskTypes = []string{
	"sum-number",
}

var ErrTaskValidationFailed = errors.New("task validation failed")

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Process(ctx context.Context, task Task) (ServiceResult, error) {
	if err := s.validateTask(task); err != nil {
		return ServiceResult{}, err
	}

	return ServiceResult{}, nil
}

func (s *Service) validateTask(task Task) error {
	if task.ID == uuid.Nil {
		return fmt.Errorf("task nil uuid provided: %w", ErrTaskValidationFailed)
	}

	if task.Type == "" {
		return fmt.Errorf("task type is empty string: %w", ErrTaskValidationFailed)
	}

	if !slices.Contains(supportedTaskTypes, task.Type) {
		return fmt.Errorf("task type is not supported: %w", ErrTaskValidationFailed)
	}

	if task.Timeout <= 0 {
		return fmt.Errorf("task timeout is zero or less: %w", ErrTaskValidationFailed)
	}

	if task.AttemptNumber <= 0 {
		return fmt.Errorf("task timeout is zero or less: %w", ErrTaskValidationFailed)
	}

	return nil
}
