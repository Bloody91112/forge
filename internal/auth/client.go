package auth

import (
	"time"

	"github.com/google/uuid"
)

type Client struct {
	ID        uuid.UUID
	Name      string
	TokenHash string
	CreatedAt time.Time
}
