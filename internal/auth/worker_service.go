package auth

import (
	"context"
	"errors"
	"fmt"
	"forge/internal/token"
	"forge/internal/worker"
	"net/http"

	"github.com/google/uuid"
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

func (s *WorkerService) VerifyWorkerByToken(r *http.Request) (uuid.UUID, error) {
	workerToken, err := token.ExtractBearerToken(r)
	if err != nil {
		return uuid.Nil, fmt.Errorf("extract worker token: %w", err)
	}
	hash := token.HashToken(workerToken)

	foundWorker, err := s.repo.GetWorkerByTokenHash(r.Context(), hash)

	if err != nil {
		return uuid.Nil, fmt.Errorf("get worker by token: %w", err)
	}

	return foundWorker.ID, nil
}
