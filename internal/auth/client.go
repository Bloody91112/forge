package auth

import (
	"time"

	"github.com/google/uuid"
)

type Client struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	TokenHash string    `json:"token_hash"`
	CreatedAt time.Time `json:"created_at"`
}
