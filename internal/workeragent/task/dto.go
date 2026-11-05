package task

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Task struct {
	ID            uuid.UUID
	Type          string
	Payload       json.RawMessage
	AttemptNumber int64
	Timeout       time.Duration
}

type ServiceError struct {
	Code      string
	Message   string
	Retryable bool
}

type ServiceResult struct {
	Error  *ServiceError
	Result any
}
