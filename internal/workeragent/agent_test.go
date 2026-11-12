package workeragent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forge/internal/workeragent/forgeapi"
	"forge/internal/workeragent/task"

	"github.com/google/uuid"
)

type fakeForgeClient struct {
	registerCalls  atomic.Int32
	heartbeatCalls atomic.Int32
	claimCalls     atomic.Int32
	renewCalls     atomic.Int32
	completeCalls  atomic.Int32
	failCalls      atomic.Int32

	heartbeat func(context.Context, string, forgeapi.HeartbeatRequest) (forgeapi.HeartbeatResponse, error)
	claim     func(context.Context, string, forgeapi.ClaimTaskRequest) (*forgeapi.ClaimTaskResponse, error)
	renew     func(context.Context, string, uuid.UUID, forgeapi.RenewTaskRequest) (forgeapi.RenewTaskResponse, error)
	complete  func(context.Context, string, uuid.UUID, forgeapi.CompleteTaskRequest) error
	fail      func(context.Context, string, uuid.UUID, forgeapi.FailTaskRequest) error
}

func (c *fakeForgeClient) Register(_ context.Context, _ string, input forgeapi.RegisterRequest) (forgeapi.RegisterResponse, error) {
	attempt := c.registerCalls.Add(1)
	return forgeapi.RegisterResponse{
		WorkerID:    input.WorkerID,
		WorkerToken: fmt.Sprintf("worker-token-%d", attempt),
	}, nil
}

func (c *fakeForgeClient) Heartbeat(ctx context.Context, token string, input forgeapi.HeartbeatRequest) (forgeapi.HeartbeatResponse, error) {
	c.heartbeatCalls.Add(1)
	if c.heartbeat != nil {
		return c.heartbeat(ctx, token, input)
	}
	return forgeapi.HeartbeatResponse{WorkerID: input.WorkerID}, nil
}

func (c *fakeForgeClient) ClaimTask(ctx context.Context, token string, input forgeapi.ClaimTaskRequest) (*forgeapi.ClaimTaskResponse, error) {
	call := c.claimCalls.Add(1)
	if c.claim != nil {
		return c.claim(ctx, token, input)
	}
	if call == 1 {
		return &forgeapi.ClaimTaskResponse{Task: newClaimedTask()}, nil
	}
	return nil, nil
}

func (c *fakeForgeClient) RenewTaskLease(ctx context.Context, token string, id uuid.UUID, input forgeapi.RenewTaskRequest) (forgeapi.RenewTaskResponse, error) {
	c.renewCalls.Add(1)
	if c.renew != nil {
		return c.renew(ctx, token, id, input)
	}
	return forgeapi.RenewTaskResponse{}, nil
}

func (c *fakeForgeClient) CompleteTask(ctx context.Context, token string, id uuid.UUID, input forgeapi.CompleteTaskRequest) error {
	c.completeCalls.Add(1)
	if c.complete != nil {
		return c.complete(ctx, token, id, input)
	}
	return nil
}

func (c *fakeForgeClient) FailTask(ctx context.Context, token string, id uuid.UUID, input forgeapi.FailTaskRequest) error {
	c.failCalls.Add(1)
	if c.fail != nil {
		return c.fail(ctx, token, id, input)
	}
	return nil
}

type fakeTaskService struct {
	process func(context.Context, task.Task) (task.ServiceResult, error)
}

func (s fakeTaskService) Process(ctx context.Context, input task.Task) (task.ServiceResult, error) {
	if s.process != nil {
		return s.process(ctx, input)
	}
	return task.ServiceResult{Result: map[string]any{"ok": true}}, nil
}

func newClaimedTask() forgeapi.ClaimedTask {
	return forgeapi.ClaimedTask{
		ID:             uuid.New(),
		Type:           "test.task",
		Payload:        []byte(`{"value":1}`),
		AttemptNumber:  1,
		TimeoutSeconds: 30,
		LeaseToken:     "lease-token",
	}
}

func newTestAgent(client *fakeForgeClient, processor TaskService) *Agent {
	agent := NewAgent(client, processor, "bootstrap-token", []string{"test.task"})
	agent.intervals = agentIntervals{
		heartbeat: 5 * time.Millisecond,
		claim:     5 * time.Millisecond,
		renew:     5 * time.Millisecond,
	}
	return agent
}

func startAgent(t *testing.T, agent *Agent) (context.CancelFunc, <-chan error) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()
	return cancel, done
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not satisfied before timeout")
}

func stopAgent(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Agent.Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Agent.Run() did not stop after context cancellation")
	}
}

func TestAgentRunsTasksSequentially(t *testing.T) {
	client := &fakeForgeClient{}
	agent := newTestAgent(client, fakeTaskService{})
	cancel, done := startAgent(t, agent)

	waitFor(t, func() bool { return client.claimCalls.Load() >= 2 })
	stopAgent(t, cancel, done)

	if got := client.completeCalls.Load(); got != 1 {
		t.Errorf("complete calls = %d, want 1", got)
	}
	if got := client.failCalls.Load(); got != 0 {
		t.Errorf("fail calls = %d, want 0", got)
	}
}

func TestAgentRenewsLeaseWhileTaskRunsAndStopsAfterCompletion(t *testing.T) {
	client := &fakeForgeClient{}
	processor := fakeTaskService{process: func(ctx context.Context, _ task.Task) (task.ServiceResult, error) {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		deadline := time.NewTimer(time.Second)
		defer deadline.Stop()

		for client.renewCalls.Load() < 2 {
			select {
			case <-ctx.Done():
				return task.ServiceResult{}, ctx.Err()
			case <-deadline.C:
				return task.ServiceResult{}, errors.New("renew cycle did not run twice")
			case <-ticker.C:
			}
		}

		return task.ServiceResult{Result: map[string]any{"ok": true}}, nil
	}}
	agent := newTestAgent(client, processor)
	cancel, done := startAgent(t, agent)

	waitFor(t, func() bool { return client.completeCalls.Load() == 1 })
	stopAgent(t, cancel, done)

	callsAfterStop := client.renewCalls.Load()
	time.Sleep(20 * time.Millisecond)
	if got := client.renewCalls.Load(); got != callsAfterStop {
		t.Errorf("renew continued after task completion: calls before=%d after=%d", callsAfterStop, got)
	}
	if callsAfterStop < 2 {
		t.Errorf("renew calls = %d, want at least 2", callsAfterStop)
	}
}

func TestRenewUnauthorizedRefreshesWorkerToken(t *testing.T) {
	client := &fakeForgeClient{}
	client.renew = func(_ context.Context, token string, _ uuid.UUID, _ forgeapi.RenewTaskRequest) (forgeapi.RenewTaskResponse, error) {
		if token == "worker-token-1" {
			return forgeapi.RenewTaskResponse{}, &forgeapi.RemoteError{StatusCode: 401, Code: "unauthorized"}
		}
		return forgeapi.RenewTaskResponse{}, nil
	}

	processor := fakeTaskService{process: func(ctx context.Context, _ task.Task) (task.ServiceResult, error) {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		deadline := time.NewTimer(time.Second)
		defer deadline.Stop()

		for client.renewCalls.Load() < 2 {
			select {
			case <-ctx.Done():
				return task.ServiceResult{}, ctx.Err()
			case <-deadline.C:
				return task.ServiceResult{}, errors.New("renew cycle did not continue after authorization refresh")
			case <-ticker.C:
			}
		}

		return task.ServiceResult{Result: map[string]any{"ok": true}}, nil
	}}
	agent := newTestAgent(client, processor)
	cancel, done := startAgent(t, agent)

	waitFor(t, func() bool { return client.completeCalls.Load() == 1 })
	stopAgent(t, cancel, done)

	if got := client.registerCalls.Load(); got != 2 {
		t.Errorf("register calls = %d, want 2", got)
	}
}

func TestAgentStopsProcessingAfterLeaseLostAndContinues(t *testing.T) {
	client := &fakeForgeClient{}
	client.renew = func(context.Context, string, uuid.UUID, forgeapi.RenewTaskRequest) (forgeapi.RenewTaskResponse, error) {
		return forgeapi.RenewTaskResponse{}, &forgeapi.RemoteError{
			StatusCode: 409,
			Code:       "lease_lost",
			Message:    "task lease is no longer valid",
		}
	}

	processor := fakeTaskService{process: func(ctx context.Context, _ task.Task) (task.ServiceResult, error) {
		<-ctx.Done()
		return task.ServiceResult{}, ctx.Err()
	}}
	agent := newTestAgent(client, processor)
	cancel, done := startAgent(t, agent)

	waitFor(t, func() bool { return client.claimCalls.Load() >= 2 })
	stopAgent(t, cancel, done)

	if got := client.renewCalls.Load(); got != 1 {
		t.Errorf("renew calls = %d, want 1", got)
	}
	if got := client.completeCalls.Load(); got != 0 {
		t.Errorf("complete calls = %d, want 0", got)
	}
	if got := client.failCalls.Load(); got != 0 {
		t.Errorf("fail calls = %d, want 0", got)
	}
}

func TestAgentReportsTaskProcessingErrorAndContinues(t *testing.T) {
	client := &fakeForgeClient{}
	processor := fakeTaskService{process: func(context.Context, task.Task) (task.ServiceResult, error) {
		return task.ServiceResult{}, errors.New("handler failed")
	}}
	agent := newTestAgent(client, processor)
	cancel, done := startAgent(t, agent)

	waitFor(t, func() bool { return client.claimCalls.Load() >= 2 })
	stopAgent(t, cancel, done)

	if got := client.failCalls.Load(); got != 1 {
		t.Errorf("fail calls = %d, want 1", got)
	}
	if got := client.completeCalls.Load(); got != 0 {
		t.Errorf("complete calls = %d, want 0", got)
	}
}

func TestHeartbeatFailureDoesNotStopAgent(t *testing.T) {
	client := &fakeForgeClient{}
	client.heartbeat = func(_ context.Context, _ string, input forgeapi.HeartbeatRequest) (forgeapi.HeartbeatResponse, error) {
		return forgeapi.HeartbeatResponse{}, &forgeapi.RemoteError{StatusCode: 500, Code: "internal_error"}
	}
	agent := newTestAgent(client, fakeTaskService{})
	cancel, done := startAgent(t, agent)

	waitFor(t, func() bool {
		return client.heartbeatCalls.Load() >= 2 && client.claimCalls.Load() >= 2
	})
	stopAgent(t, cancel, done)
}

func TestClaimUnauthorizedRefreshesWorkerToken(t *testing.T) {
	client := &fakeForgeClient{}
	client.claim = func(_ context.Context, token string, _ forgeapi.ClaimTaskRequest) (*forgeapi.ClaimTaskResponse, error) {
		if token == "worker-token-1" {
			return nil, &forgeapi.RemoteError{StatusCode: 401, Code: "unauthorized"}
		}
		if client.claimCalls.Load() == 2 {
			return &forgeapi.ClaimTaskResponse{Task: newClaimedTask()}, nil
		}
		return nil, nil
	}
	agent := newTestAgent(client, fakeTaskService{})
	cancel, done := startAgent(t, agent)

	waitFor(t, func() bool { return client.completeCalls.Load() >= 1 })
	stopAgent(t, cancel, done)

	if got := client.registerCalls.Load(); got != 2 {
		t.Errorf("register calls = %d, want 2", got)
	}
}

func TestCompleteUnauthorizedRefreshesAndRetries(t *testing.T) {
	client := &fakeForgeClient{}
	var firstToken string
	client.complete = func(_ context.Context, token string, _ uuid.UUID, _ forgeapi.CompleteTaskRequest) error {
		if firstToken == "" {
			firstToken = token
			return &forgeapi.RemoteError{StatusCode: 401, Code: "unauthorized"}
		}
		if token == firstToken {
			return errors.New("completion retried with stale worker token")
		}
		return nil
	}
	agent := newTestAgent(client, fakeTaskService{})
	cancel, done := startAgent(t, agent)

	waitFor(t, func() bool { return client.registerCalls.Load() == 2 && client.completeCalls.Load() >= 2 })
	stopAgent(t, cancel, done)
}

func TestConcurrentUnauthorizedResponsesTriggerOneReregistration(t *testing.T) {
	client := &fakeForgeClient{}
	agent := newTestAgent(client, fakeTaskService{})
	if err := agent.registerAtForge(context.Background()); err != nil {
		t.Fatalf("initial register: %v", err)
	}
	rejectedToken := agent.getWorkerToken()

	const callers = 8
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- agent.refreshWorkerToken(context.Background(), rejectedToken)
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("refreshWorkerToken() error = %v", err)
		}
	}
	if got := client.registerCalls.Load(); got != 2 {
		t.Errorf("register calls = %d, want 2", got)
	}
}

func TestMarshalTaskResultRequiresObject(t *testing.T) {
	if _, err := marshalTaskResult(map[string]any{"ok": true}); err != nil {
		t.Fatalf("object result rejected: %v", err)
	}
	if _, err := marshalTaskResult(nil); err == nil {
		t.Fatal("nil result should be rejected")
	}
	if _, err := marshalTaskResult([]int{1, 2}); err == nil {
		t.Fatal("array result should be rejected")
	}
	if _, err := marshalTaskResult(map[string]any{"data": make([]byte, maxTaskResultSize)}); err == nil {
		t.Fatal("result larger than 1 MiB should be rejected")
	}
}
