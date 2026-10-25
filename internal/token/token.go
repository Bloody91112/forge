package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var ErrInvalidAuthToken = errors.New("invalid or missing authorization header")

const bootstrapTokenBytes = 32

func GenerateBootstrapToken() (string, error) {
	token := make([]byte, bootstrapTokenBytes)

	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("generate bootstrap token: %w", err)
	}

	return hex.EncodeToString(token), nil
}

func GenerateWorkerToken() (string, error) {
	token := make([]byte, bootstrapTokenBytes)

	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("generate worker token: %w", err)
	}

	return hex.EncodeToString(token), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func ExtractBearerToken(r *http.Request) (string, error) {
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
