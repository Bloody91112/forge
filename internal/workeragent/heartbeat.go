package workeragent

import (
	"context"
	"fmt"
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
			workerToken := a.getWorkerToken()
			response, err := a.client.Heartbeat(ctx, workerToken, forgeapi.HeartbeatRequest{WorkerID: a.workerID})
			if ctx.Err() != nil {
				return nil
			}

			if isForgeAuthFailedError(err) {
				if refreshErr := a.refreshWorkerToken(ctx, workerToken); refreshErr != nil {
					return fmt.Errorf("refresh worker authorization after heartbeat: %w", refreshErr)
				}
				continue
			}

			if err != nil {
				log.Printf("worker heartbeat failed: %v", err)
				continue
			}

			if response.WorkerID != a.workerID {
				log.Printf("worker heartbeat returned unexpected worker id: got %s, want %s", response.WorkerID, a.workerID)
			}
		}
	}
}
