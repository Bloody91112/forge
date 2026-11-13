package auth

import (
	"context"
	"errors"
	"forge/internal/token"
	"net/http"

	"github.com/google/uuid"
)

var ErrClientNotFound = errors.New("client not found")
var ErrClientInvalidParam = errors.New("client invalid param provided")

type ClientRepository interface {
	GetClientByTokenHash(ctx context.Context, hash string) (Client, error)
	CreateClient(ctx context.Context, client Client) (Client, error)
	RotateClientToken(ctx context.Context, id uuid.UUID, tokenHash string) (Client, error)
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

func (s *ClientService) CreateClient(ctx context.Context, name string) (Client, string, error) {
	clientToken, err := token.GenerateClientToken()
	if err != nil {
		return Client{}, "", err
	}

	client := Client{
		ID:        uuid.New(),
		Name:      name,
		TokenHash: token.HashToken(clientToken),
	}

	createdClient, err := s.repo.CreateClient(ctx, client)
	if err != nil {
		return Client{}, "", err
	}

	return createdClient, clientToken, nil
}

func (s *ClientService) RotateClientToken(ctx context.Context, id uuid.UUID) (Client, string, error) {
	clientToken, err := token.GenerateClientToken()
	if err != nil {
		return Client{}, "", err
	}

	rotatedClient, err := s.repo.RotateClientToken(ctx, id, token.HashToken(clientToken))
	if err != nil {
		return Client{}, "", err
	}

	return rotatedClient, clientToken, nil
}
