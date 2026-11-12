package workeragent

import (
	"context"
	"errors"
	"fmt"

	"forge/internal/workeragent/forgeapi"
)

type registrationState struct {
	rejectedToken string
	attempts      int
	lastErr       error
	exhaustedErr  error
}

func (a *Agent) registerAtForge(ctx context.Context) error {
	token, err := a.registerWorker(ctx)
	if err != nil {
		return err
	}

	a.setWorkerToken(token)
	a.registrationMu.Lock()
	a.registration = registrationState{}
	a.registrationMu.Unlock()

	return nil
}

func (a *Agent) registerWorker(ctx context.Context) (string, error) {
	request := forgeapi.RegisterRequest{
		WorkerID:        a.workerID,
		ProtocolVersion: Protocol,
		Capabilities:    a.capabilities,
	}

	response, err := a.client.Register(ctx, a.bootstrapToken, request)
	if err != nil {
		return "", fmt.Errorf("register worker: %w", err)
	}

	if response.WorkerID != a.workerID {
		return "", ErrWorkerIDConflict
	}
	if response.WorkerToken == "" {
		return "", errors.New("register worker: Forge returned an empty worker token")
	}

	return response.WorkerToken, nil
}

func (a *Agent) refreshWorkerToken(ctx context.Context, rejectedToken string) error {
	a.registrationMu.Lock()
	defer a.registrationMu.Unlock()

	if a.workerToken != rejectedToken {
		return nil
	}

	if a.registration.rejectedToken != rejectedToken {
		a.registration = registrationState{rejectedToken: rejectedToken}
	}

	if a.registration.exhaustedErr != nil {
		return a.registration.exhaustedErr
	}

	for a.registration.attempts < 3 {
		if err := ctx.Err(); err != nil {
			return err
		}

		a.registration.attempts++
		token, err := a.registerWorker(ctx)
		if err != nil {
			a.registration.lastErr = err
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}

		a.setWorkerToken(token)
		a.registration = registrationState{}
		return nil
	}

	a.registration.exhaustedErr = fmt.Errorf(
		"worker re-registration failed after 3 attempts: %w",
		a.registration.lastErr,
	)

	return a.registration.exhaustedErr
}

func (a *Agent) withWorkerAuthorizationRetry(ctx context.Context, call func(workerToken string) error) error {
	token := a.getWorkerToken()
	if err := call(token); !isForgeAuthFailedError(err) {
		return err
	}

	if err := a.refreshWorkerToken(ctx, token); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %w", errWorkerAuthorizationFailed, err)
	}

	err := call(a.getWorkerToken())
	if isForgeAuthFailedError(err) {
		return fmt.Errorf("%w: Forge rejected the refreshed worker token: %w", errWorkerAuthorizationFailed, err)
	}

	return err
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
