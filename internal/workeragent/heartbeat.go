package workeragent

import (
	"context"
	"log"
	"time"

	"forge/internal/workeragent/forgeapi"
)

func (a *Agent) runHeartbeatCycle(ctx context.Context) error {
	ticker := time.NewTicker(a.intervals.heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-ticker.C:
			var response forgeapi.HeartbeatResponse
			err := a.withWorkerAuthorizationRetry(ctx, func(workerToken string) error {
				var requestErr error
				response, requestErr = a.client.Heartbeat(
					ctx,
					workerToken,
					forgeapi.HeartbeatRequest{WorkerID: a.workerID},
				)
				return requestErr
			})

			if ctx.Err() != nil {
				return nil
			}
			if isWorkerAuthorizationFailedError(err) {
				return err
			}
			if err != nil {
				log.Printf("worker heartbeat failed: %v", err)
				continue
			}

			if response.WorkerID != a.workerID {
				log.Printf(
					"worker heartbeat returned unexpected worker id: got %s, want %s",
					response.WorkerID,
					a.workerID,
				)
			}
		}
	}
}
