package workeragent

import (
	"context"
	"errors"
	"fmt"

	"forge/internal/workeragent/forgeapi"
)

func (a *Agent) registerAtForge(ctx context.Context) error {
	request := forgeapi.RegisterRequest{
		WorkerID:        a.workerID,
		ProtocolVersion: Protocol,
		Capabilities:    a.capabilities,
	}

	response, err := a.client.Register(ctx, a.bootstrapToken, request)
	if err != nil {
		return fmt.Errorf("register worker: %w", err)
	}

	if response.WorkerID != a.workerID {
		return ErrWorkerIDConflict
	}
	if response.WorkerToken == "" {
		return errors.New("register worker: Forge returned an empty worker token")
	}

	a.setWorkerToken(response.WorkerToken)
	return nil
}

func (a *Agent) refreshWorkerToken(ctx context.Context, rejectedToken string) error {
	a.registrationMu.Lock()
	defer a.registrationMu.Unlock()

	if a.getWorkerToken() != rejectedToken {
		return nil
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := a.registerAtForge(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}

	return fmt.Errorf("worker re-registration failed after 3 attempts: %w", lastErr)
}

func (a *Agent) withWorkerAuthorizationRetry(ctx context.Context, call func(workerToken string) error) error {
	token := a.getWorkerToken()
	if err := call(token); !isForgeAuthFailedError(err) {
		return err
	}

	if err := a.refreshWorkerToken(ctx, token); err != nil {
		return err
	}

	return call(a.getWorkerToken())
}

func (a *Agent) getWorkerToken() string {
	a.tokenMu.RLock()
	defer a.tokenMu.RUnlock()

	return a.workerToken
}

func (a *Agent) setWorkerToken(token string) {
	a.tokenMu.Lock()
	defer a.tokenMu.Unlock()

	a.workerToken = token
}
