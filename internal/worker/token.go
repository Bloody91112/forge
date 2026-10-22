package worker

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const bootstrapTokenBytes = 32

func GenerateBootstrapToken() (string, error) {
	token := make([]byte, bootstrapTokenBytes)

	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("generate bootstrap token: %w", err)
	}

	return hex.EncodeToString(token), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
