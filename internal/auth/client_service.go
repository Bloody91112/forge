package auth

import (
	"context"
	"errors"
	"forge/internal/token"
	"net/http"

	"github.com/google/uuid"
)

var ErrClientNotFound = errors.New("client not found")

type ClientRepository interface {
	GetClientByTokenHash(ctx context.Context, hash string) (Client, error)
}

type ClientService struct {
	repo ClientRepository
}

func NewClientService(repo ClientRepository) *ClientService {
	return &ClientService{repo: repo}
}

func (s *ClientService) VerifyClientByToken(r *http.Request) (uuid.UUID, error) {
	clientToken, err := token.ExtractBearerToken(r)
	if err != nil {
		return uuid.Nil, err
	}
	hash := token.HashToken(clientToken)
	client, err := s.repo.GetClientByTokenHash(r.Context(), hash)
	if err != nil {
		return uuid.Nil, err
	}

	return client.ID, nil
}
