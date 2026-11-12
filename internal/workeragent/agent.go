package workeragent

import (
	"context"
	"errors"
	"sync"
	"time"

	"forge/internal/workeragent/forgeapi"
	"forge/internal/workeragent/task"

	"github.com/google/uuid"
)

const (
	Protocol                    = "1"
	HeartbeatPeriodSeconds      = 10
	ClaimTaskPeriodSeconds      = 10
	RenewTaskLeasePeriodSeconds = 10
)

var ErrWorkerIDConflict = errors.New("worker id conflict")

type ForgeClient interface {
	Register(ctx context.Context, bootstrapToken string, input forgeapi.RegisterRequest) (forgeapi.RegisterResponse, error)
	Heartbeat(ctx context.Context, workerToken string, input forgeapi.HeartbeatRequest) (forgeapi.HeartbeatResponse, error)
	ClaimTask(ctx context.Context, workerToken string, input forgeapi.ClaimTaskRequest) (*forgeapi.ClaimTaskResponse, error)
	RenewTaskLease(ctx context.Context, workerToken string, taskID uuid.UUID, input forgeapi.RenewTaskRequest) (forgeapi.RenewTaskResponse, error)
	CompleteTask(ctx context.Context, workerToken string, taskID uuid.UUID, input forgeapi.CompleteTaskRequest) error
	FailTask(ctx context.Context, workerToken string, taskID uuid.UUID, input forgeapi.FailTaskRequest) error
}

type TaskService interface {
	Process(ctx context.Context, task task.Task) (task.ServiceResult, error)
}

type agentIntervals struct {
	heartbeat time.Duration
	claim     time.Duration
	renew     time.Duration
}

type Agent struct {
	client         ForgeClient
	taskService    TaskService
	bootstrapToken string
	capabilities   []string
	workerID       uuid.UUID

	tokenMu        sync.RWMutex
	workerToken    string
	registrationMu sync.Mutex
	registration   registrationState

	intervals agentIntervals
}

func NewAgent(client ForgeClient, taskService TaskService, bootstrapToken string, capabilities []string) *Agent {
	return &Agent{
		client:         client,
		taskService:    taskService,
		bootstrapToken: bootstrapToken,
		capabilities:   append([]string(nil), capabilities...),
		workerID:       uuid.New(),
		intervals: agentIntervals{
			heartbeat: HeartbeatPeriodSeconds * time.Second,
			claim:     ClaimTaskPeriodSeconds * time.Second,
			renew:     RenewTaskLeasePeriodSeconds * time.Second,
		},
	}
}

func (a *Agent) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil
	}

	if err := a.registerAtForge(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	heartbeatErr := make(chan error, 1)
	heartbeatDone := make(chan struct{})

	go func() {
		defer close(heartbeatDone)
		if err := a.runHeartbeatCycle(runCtx); err != nil {
			heartbeatErr <- err
			cancel()
		}
	}()

	defer func() {
		cancel()
		<-heartbeatDone
	}()

	for {
		claimedTask, err := a.waitForTask(runCtx)
		if err != nil {
			return a.shutdownError(ctx, heartbeatErr, err)
		}

		if err := a.runTask(runCtx, *claimedTask); err != nil {
			return a.shutdownError(ctx, heartbeatErr, err)
		}
	}
}

func (a *Agent) shutdownError(parentCtx context.Context, heartbeatErr <-chan error, fallback error) error {
	if parentCtx.Err() != nil {
		return nil
	}

	select {
	case err := <-heartbeatErr:
		if err != nil {
			return err
		}
	default:
	}

	return fallback
}
