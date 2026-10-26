package worker

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type RegisterRequestDTO struct {
	ID              uuid.UUID       `json:"worker_id"`
	ProtocolVersion string          `json:"protocol_version"`
	Capabilities    json.RawMessage `json:"capabilities"`
}

type RegisterResponseDTO struct {
	ID             uuid.UUID `json:"worker_id"`
	Token          string    `json:"worker_token"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}
