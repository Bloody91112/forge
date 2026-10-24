package worker

import (
	"time"

	"github.com/google/uuid"
)

type Worker struct {
	ID              uuid.UUID
	TokenHash       string
	ProtocolVersion string
	Capabilities    []string
	RegisteredAt    time.Time
	LastSeenAt      *time.Time
	LeaseExpiresAt  time.Time
}
