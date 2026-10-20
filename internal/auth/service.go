package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

var ErrClientNotFound = errors.New("client not found")

type Repository interface {
	GetClientIDByTokenHash(ctx context.Context, hash string) (uuid.UUID, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

var ErrInvalidAuthToken = errors.New("invalid or missing authorization header")

func (s *Service) VerifyClientByToken(r *http.Request) (uuid.UUID, error) {
	token, err := extractBearerToken(r)
	if err != nil {
		return uuid.Nil, err
	}
	hash := makeTokenHash(token)
	return s.repo.GetClientIDByTokenHash(r.Context(), hash)

}

func makeTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func extractBearerToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", ErrInvalidAuthToken
	}

	parts := strings.Fields(authHeader)

	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", ErrInvalidAuthToken
	}

	return parts[1], nil
}
