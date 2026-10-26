package worker

import (
	"time"

	"github.com/google/uuid"
)

type HeartbeatRequestDTO struct {
	WorkerID uuid.UUID `json:"worker_id"`
}

type HeartbeatResponseDTO struct {
	WorkerID       uuid.UUID `json:"worker_id"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}
