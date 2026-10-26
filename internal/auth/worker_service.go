package auth

import (
	"context"
	"errors"
	"fmt"
	"forge/internal/token"
	"forge/internal/worker"
	"net/http"
)

var ErrWorkerNotFound = errors.New("worker not found")

type WorkerRepository interface {
	GetWorkerByTokenHash(ctx context.Context, hash string) (worker.Worker, error)
}

type WorkerService struct {
	repo WorkerRepository
}

func NewWorkerService(repo WorkerRepository) *WorkerService {
	return &WorkerService{repo: repo}
}

func (s *WorkerService) VerifyWorkerByToken(r *http.Request) error {
	workerToken, err := token.ExtractBearerToken(r)
	if err != nil {
		return fmt.Errorf("extract worker token: %w", err)
	}
	hash := token.HashToken(workerToken)

	if _, err := s.repo.GetWorkerByTokenHash(r.Context(), hash); err != nil {
		return fmt.Errorf("get worker by token: %w", err)
	}

	return nil
}
