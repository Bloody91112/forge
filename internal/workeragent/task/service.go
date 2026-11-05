package task

import (
	"context"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Process(ctx context.Context, task Task) (ServiceResult, error) {
	return ServiceResult{}, nil
}
