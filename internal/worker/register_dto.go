package worker

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type RegisterWorkerInput struct {
	ID              uuid.UUID       `json:"id"`
	BootstrapToken  string          `json:"bootstrap_token"`
	ProtocolVersion string          `json:"protocol_version"`
	Capabilities    json.RawMessage `json:"capabilities"`
}

type RegisterWorkerOutput struct {
	ID             uuid.UUID `json:"worker_id"`
	Token          string    `json:"worker_token"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}
