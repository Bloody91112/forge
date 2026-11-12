package task

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestProcessExpiredContextReturnsTimeoutResult(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	service := NewService(nil)
	result, err := service.Process(ctx, Task{
		ID:            uuid.New(),
		Type:          "test.task",
		AttemptNumber: 1,
		Timeout:       time.Second,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Error == nil {
		t.Fatal("Process() returned no task error for an expired deadline")
	}
	if result.Error.Code != ErrorCodeTaskTimeout {
		t.Errorf("error code = %q, want %q", result.Error.Code, ErrorCodeTaskTimeout)
	}
	if !result.Error.Retryable {
		t.Error("timeout should be retryable")
	}
}
