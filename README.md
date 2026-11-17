
# Forge

Forge is a background task processing system written in Go. It provides an HTTP API for submitting tasks, persists them in PostgreSQL and distributes their execution across independent Worker applications.

Forge separates task management from task execution. It does not execute application code or interpret task payloads. Workers are responsible for implementing task-specific business logic and can be written in any programming language.

## Architecture

Forge consists of three independent components:

- **Forge** — HTTP API responsible for task creation, scheduling, execution coordination, retries and task state management.
- **PostgreSQL** — persistent storage for tasks, execution attempts, client credentials and Worker registrations.
- **Workers** — independent applications that register with Forge, claim tasks, execute them and report their results.

```text
                    +----------------+
                    |     Client     |
                    +-------+--------+
                            |
                         HTTP API
                            |
                            v
                    +-------+--------+
                    |     Forge      |
                    +-------+--------+
                            |
                       PostgreSQL
                            |
                 Internal HTTP API
                            |
                 +----------+----------+
                 |                     |
           +-----+------+        +-----+------+
           |   Worker   |        |   Worker   |
           |  (Go)      |        | (Other)    |
           +------------+        +------------+
```

Workers communicate with Forge exclusively through its internal HTTP API. They do not access PostgreSQL directly.

Forge and Workers can be developed, deployed and scaled independently. No message broker is required.

## Task processing

The typical processing flow:

1. A client submits a task through the public HTTP API.
2. Forge validates the request and persists the task in PostgreSQL.
3. An available Worker claims the task through the internal HTTP API.
4. The Worker executes the task using its own implementation.
5. The Worker reports the result or an execution error to Forge.
6. Forge updates the task state and records the execution attempt.

Tasks have four possible states:

| Status | Description |
|---|---|
| `queued` | Waiting for execution or retry |
| `running` | Currently being processed |
| `succeeded` | Completed successfully |
| `failed` | Failed permanently or exhausted all attempts |

Forge provides:

- Persistent task storage.
- Idempotent task creation.
- Worker registration and capability management.
- Task execution leases.
- Automatic recovery of expired leases.
- Retry handling with exponential backoff and full jitter.
- Execution attempt history.
- Client and Worker authentication.

Task delivery follows an **at-least-once** model. A task can be executed more than once. Workers must account for repeated execution and protect external side effects where necessary. Exactly-once execution of external side effects is not guaranteed.

## Components in this repository

The repository contains two Go applications:

- `cmd/forge` — Forge API and administrative CLI.
- `cmd/worker` — reference Go Worker implementation.

The reference Worker demonstrates how an independent application communicates with Forge using the HTTP protocol. It is not a library or plugin system for Forge.

External Workers do not need to use Go, import Forge packages or implement internal Go interfaces. They only need to implement the Worker HTTP protocol.

## Deployment

### Requirements

- Docker Engine
- Docker Compose
- Bash
- curl

### 1. Clone the repository

```bash
git clone https://github.com/Bloody91112/forge.git
cd forge
```

### 2. Configure the environment

The setup script creates `.env` from `.env.example` if the file does not exist.

Configure the database credentials and other required settings before deployment.

| Variable | Description |
|---|---|
| `HTTP_ADDR` | Forge HTTP listen address |
| `DB_HOST` | PostgreSQL host |
| `DB_PORT` | PostgreSQL host port |
| `DB_DATABASE` | Main database name |
| `DB_TEST_DATABASE` | Test database name |
| `DB_USERNAME` | PostgreSQL username |
| `DB_PASSWORD` | PostgreSQL password |
| `WORKER_CAPABILITIES` | Task types supported by the reference Worker |

The main and test databases must have different names.

The reference Worker currently supports the `sum-numbers` task type:

```dotenv
WORKER_CAPABILITIES=sum-numbers
```

Change the example database password before using the environment outside disposable local development.

### 3. Deploy Forge

Run the setup script:

```bash
chmod +x setup.sh
./setup.sh
```

The script prepares the environment, builds the application images, starts PostgreSQL, applies database migrations, initializes the Worker bootstrap token, starts Forge and launches the reference Worker.

After deployment, the API is available at:

`http://localhost:8080`

Health endpoints:

- `GET /healthz` — checks whether the Forge process is responding.
- `GET /readyz` — checks whether Forge is ready to serve requests.

Database migrations are executed by the setup script as part of deployment.

## Using the API

All public task API requests require a client Bearer token.

### Client credentials

Forge provides administrative CLI commands for client management.

Create a client:

```text
forge client create "my-application"
```

The command returns the client ID, name and client token.

The token is shown only when it is created. Forge stores its SHA-256 hash rather than the plaintext token. Save the token securely.

Rotate a client token:

```text
forge client rotate <client-id>
```

The previous token remains valid for five minutes after rotation.

These commands are executed by the Forge application in an environment with access to its database configuration.

### Create a task

Submit a task using `POST /api/v1/tasks`.

Example using the reference Worker:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Authorization: Bearer <client-token>" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: sum-example-001" \
  -d '{
    "type": "sum-numbers",
    "payload": {
      "number_1": 19,
      "number_2": 23
    }
  }'
```

The request body contains:

| Field | Required | Description |
|---|---|---|
| `type` | Yes | Task type |
| `payload` | Yes | JSON object containing task input |
| `timeout_seconds` | No | Execution timeout; default is 300 seconds |
| `max_attempts` | No | Maximum number of attempts; default is 5 |

A newly created task returns `202 Accepted`:

```json
{
  "id": "0199...",
  "type": "sum-numbers",
  "status": "queued",
  "created_at": "2026-10-14T12:00:00Z"
}
```

The `Idempotency-Key` header is mandatory.

Sending the same request with the same key and identical content returns the existing task with `200 OK`. Reusing the key with different content returns `409 Conflict`.

A new task is accepted only when there is an active Worker pool and every active Worker supports the requested task type. If there are no active Workers, Forge returns `503 Service Unavailable`. If at least one active Worker does not support the requested type, Forge returns `422 Unprocessable Content`.

### Retrieve a task

```bash
curl http://localhost:8080/api/v1/tasks/<task-id> \
  -H "Authorization: Bearer <client-token>"
```

The response contains the current task status, attempt count, result or error information, and timestamps.

Example:

```json
{
  "id": "0199...",
  "type": "sum-numbers",
  "status": "succeeded",
  "attempt_count": 1,
  "result": {
    "sum": 42
  },
  "error": null,
  "created_at": "2026-10-14T12:00:00Z",
  "started_at": "2026-10-14T12:00:05Z",
  "finished_at": "2026-10-14T12:00:12Z"
}
```

### Retrieve execution history

```bash
curl http://localhost:8080/api/v1/tasks/<task-id>/attempts \
  -H "Authorization: Bearer <client-token>"
```

The response contains the execution attempts, including their outcomes, timestamps and errors.

## Public API reference

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/v1/tasks` | Create a task |
| `GET` | `/api/v1/tasks/{id}` | Retrieve task status and result |
| `GET` | `/api/v1/tasks/{id}/attempts` | Retrieve execution history |
| `GET` | `/healthz` | Check process liveness |
| `GET` | `/readyz` | Check API readiness |

The public task endpoints require client authentication. Health and readiness endpoints do not require authentication.

## Integrating an external Worker

A Worker is an independent application that communicates with Forge over HTTP. It can be implemented in any programming language and deployed separately from Forge.

The Worker protocol uses the `/internal/v1/` endpoint prefix.

### 1. Register the Worker

On startup, the Worker generates a new UUID for its process and registers with Forge:

```http
POST /internal/v1/workers/register
Authorization: Bearer <bootstrap-token>
Content-Type: application/json
```

Request:

```json
{
  "worker_id": "<worker-uuid>",
  "protocol_version": "1",
  "capabilities": [
    "sum-numbers",
    "report.generate"
  ]
}
```

Response:

```json
{
  "worker_id": "<worker-uuid>",
  "worker_token": "<worker-token>",
  "lease_expires_at": "2026-10-24T12:00:30Z"
}
```

The bootstrap token is used only for registration. Forge issues an individual Worker token, which must be used for subsequent requests.

The Worker token is shown only in the registration response. It must be kept securely.

Each Worker process must use its own UUID. Re-registering the same UUID is allowed sequentially and issues a new token, immediately invalidating the previous one.

### 2. Send heartbeats

The Worker periodically notifies Forge that it is active:

```http
POST /internal/v1/workers/heartbeat
Authorization: Bearer <worker-token>
Content-Type: application/json
```

Request:

```json
{
  "worker_id": "<worker-uuid>"
}
```

The reference implementation sends heartbeats every 10 seconds. A Worker registration expires after 30 seconds without a successful heartbeat and is then excluded from the active pool.

### 3. Claim a task

The Worker requests an available task:

```http
POST /internal/v1/tasks/claim
Authorization: Bearer <worker-token>
Content-Type: application/json
```

Request:

```json
{
  "worker_id": "<worker-uuid>"
}
```

When a task is available, Forge returns `200 OK`:

```json
{
  "task": {
    "id": "<task-uuid>",
    "type": "report.generate",
    "payload": {
      "report_id": 123
    },
    "attempt_number": 1,
    "timeout_seconds": 300,
    "lease_token": "<lease-token>",
    "lease_expires_at": "2026-10-24T12:00:30Z"
  }
}
```

If no task is available, Forge returns `204 No Content` without a response body.

The Worker must execute the task according to its `type` and `payload`.

### 4. Renew the task lease

A task has a limited execution lease. The Worker must renew it while processing the task:

```http
POST /internal/v1/tasks/{id}/renew
Authorization: Bearer <worker-token>
Content-Type: application/json
```

Request:

```json
{
  "worker_id": "<worker-uuid>",
  "lease_token": "<lease-token>"
}
```

The lease duration is 30 seconds. The reference implementation renews it every 10 seconds.

A lease can only be renewed by the Worker holding the valid lease token. An expired or replaced lease cannot be renewed.

### 5. Report successful execution

When execution succeeds, the Worker submits the result:

```http
POST /internal/v1/tasks/{id}/complete
Authorization: Bearer <worker-token>
Content-Type: application/json
```

Request:

```json
{
  "worker_id": "<worker-uuid>",
  "lease_token": "<lease-token>",
  "result": {
    "file_url": "https://example.com/report.csv"
  }
}
```

Forge validates the lease, stores the result and marks the task as `succeeded`.

### 6. Report execution failure

When execution fails, the Worker reports the error:

```http
POST /internal/v1/tasks/{id}/fail
Authorization: Bearer <worker-token>
Content-Type: application/json
```

Request:

```json
{
  "worker_id": "<worker-uuid>",
  "lease_token": "<lease-token>",
  "error": {
    "code": "temporary_failure",
    "message": "Temporary execution failure"
  },
  "retryable": true
}
```

The `retryable` flag tells Forge whether the failure can be retried. Forge either schedules another attempt or marks the task as failed, depending on the error and remaining attempt limit.

### Worker protocol endpoints

| Method | Endpoint | Purpose |
|---|---|---|
| `POST` | `/internal/v1/workers/register` | Register a Worker |
| `POST` | `/internal/v1/workers/heartbeat` | Renew Worker registration |
| `POST` | `/internal/v1/tasks/claim` | Claim an available task |
| `POST` | `/internal/v1/tasks/{id}/renew` | Renew a task lease |
| `POST` | `/internal/v1/tasks/{id}/complete` | Submit a successful result |
| `POST` | `/internal/v1/tasks/{id}/fail` | Report an execution failure |

Worker authentication and task lease validation are separate:

- The Worker token authenticates the Worker process.
- The lease token authorizes operations on a specific task execution.

An expired lease cannot be used to modify task state, even if the Worker token is still valid.

## Reference Go Worker

The repository includes a reference Worker in `cmd/worker`. It demonstrates registration, heartbeats, task claiming, lease renewal, task execution and result reporting.

The reference implementation currently supports one task type:

`sum-numbers`

Example payload:

```json
{
  "number_1": 19,
  "number_2": 23
}
```

Expected result:

```json
{
  "sum": 42
}
```

The reference Worker processes no more than one task at a time per process. Additional Worker processes can be used to increase its processing capacity.

This concurrency limit belongs to the reference implementation, not to the Forge HTTP protocol. Independent Worker implementations may use their own concurrency models.

## Reliability and execution guarantees

- A task is considered accepted only after it has been committed to PostgreSQL.
- Task execution follows an at-least-once delivery model.
- Each claim creates a unique lease token.
- Expired leases are recovered automatically by Forge.
- Failed tasks can be retried according to their retry policy.
- Execution attempts are stored separately and can be retrieved through the public API.
- Completed tasks and their attempts are retained for 30 days.
- External side effects are not guaranteed to happen exactly once. Worker implementations must be designed with retries and repeated execution in mind.

## Production considerations

For production deployments:

- Use TLS for HTTP communication.
- Store bootstrap, client and Worker tokens securely.
- Do not expose `/internal/v1/*` to public networks.
- Restrict network access between Forge and Worker applications.
- Do not log authentication tokens or sensitive task payloads.

Forge authentication remains required even when network-level restrictions are configured.
