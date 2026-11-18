
# Forge

Forge is a background task processing system written in Go. It provides an HTTP API for submitting tasks, persists them in PostgreSQL and distributes their execution across independent Worker applications.

Forge separates task management from task execution. It does not execute application code or interpret task payloads. Workers implement task-specific business logic and can be written in any programming language.

## Architecture

Forge consists of three components:

- **Forge** — manages tasks, coordinates execution, handles retries and provides the HTTP API.
- **PostgreSQL** — stores tasks, execution attempts, client credentials and Worker registrations.
- **Workers** — independent applications that receive tasks from Forge, execute them and report results.

Workers communicate with Forge exclusively through its internal HTTP API. They do not access PostgreSQL directly.

Forge and Workers can be developed and deployed independently. No separate message broker is required.

The repository contains two Go applications:

- `cmd/forge` — Forge API and administrative CLI.
- `cmd/worker` — reference Go Worker implementation.

The reference Worker demonstrates the HTTP protocol. It is not a library or plugin system that external Workers need to extend.

## Task processing

The typical processing flow:

1. A client submits a task through the public HTTP API.
2. Forge validates the request and persists the task in PostgreSQL.
3. An available Worker claims the task through the internal HTTP API.
4. The Worker executes the task using its own implementation.
5. The Worker reports the result or an error to Forge.
6. Forge updates the task state and records the execution attempt.

Tasks have four possible states:

| Status | Description |
|---|---|
| `queued` | Waiting for execution or retry |
| `running` | Currently being processed |
| `succeeded` | Completed successfully |
| `failed` | Failed permanently or exhausted all attempts |

Forge supports idempotent task creation, execution leases, automatic lease recovery, retries and execution attempt history.

Task delivery follows an at-least-once model. A task may be executed more than once, so Worker implementations must account for repeated execution.

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

Configure the database credentials and other required settings:

| Variable | Description |
|---|---|
| `DB_PORT` | PostgreSQL port published on the host |
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

The bootstrap token is initialized automatically by the setup script and saved to `.env`.

Compose provides the internal container addresses and HTTP listen address. The `DB_HOST`, `HTTP_ADDR` and `WORKER_FORGE_URL` defaults in `.env.example` do not need to be changed for this deployment.

### 3. Deploy Forge

Run the setup script:

```bash
chmod +x setup.sh
./setup.sh
```

The script:

1. Validates the environment and required tools.
2. Builds the Forge API and reference Worker images.
3. Starts PostgreSQL.
4. Applies migrations to the main and test databases.
5. Initializes the Worker bootstrap token.
6. Starts Forge and waits for API readiness.
7. Starts the reference Worker.

The API becomes available at:

`http://localhost:8080`

Health endpoints:

- `GET /healthz` — process liveness.
- `GET /readyz` — API readiness.

## Using the API

All public task API requests require a client Bearer token.

### 1. Create a client

The Forge CLI is included in the Forge application image. Create a client by invoking it in the running Forge container:

```bash
docker compose exec -T forge /forge client create "my-application"
```

The command returns the client ID, name and client token.

The token is displayed only when created. Forge stores its SHA-256 hash rather than the plaintext token. Save the token.

To rotate a client token:

```bash
docker compose exec -T forge /forge client rotate <client-id>
```

The previous token remains valid for five minutes after rotation.

### 2. Create a task

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

A newly created task returns `202 Accepted` with its ID.

The `Idempotency-Key` header is mandatory. Repeating a request with the same key and identical content returns the existing task with `200 OK`. Reusing the key with different content returns `409 Conflict`.

A new task is accepted only when there is an active Worker pool and every active Worker supports the requested task type. If there are no active Workers, Forge returns `503 Service Unavailable`. If at least one active Worker does not support the requested type, Forge returns `422 Unprocessable Content`.

### 3. Retrieve a task

```bash
curl http://localhost:8080/api/v1/tasks/<task-id> \
  -H "Authorization: Bearer <client-token>"
```

The response contains the task status, attempt count, result or error information and timestamps.

For the reference `sum-numbers` task, the result is:

```json
{
  "sum": 42
}
```

### 4. Retrieve execution history

```bash
curl http://localhost:8080/api/v1/tasks/<task-id>/attempts \
  -H "Authorization: Bearer <client-token>"
```

The response contains the execution attempts, including their outcomes, timestamps and errors.

## Public API

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

The Worker advertises its supported task types (`capabilities`) during registration. Forge uses these capabilities to determine whether a task can be accepted by the active Worker pool.

The Worker lifecycle consists of:

1. **Registration** — send a registration request with a process UUID, protocol version and capabilities. Forge returns an individual Worker token.
2. **Heartbeat** — periodically notify Forge that the Worker is active.
3. **Claim** — request an available task.
4. **Execution** — process the task according to its type and payload.
5. **Lease renewal** — renew the task lease while execution is in progress.
6. **Completion or failure** — report the result or execution error.

The internal HTTP API provides the following endpoints:

| Method | Endpoint | Purpose |
|---|---|---|
| `POST` | `/internal/v1/workers/register` | Register a Worker |
| `POST` | `/internal/v1/workers/heartbeat` | Renew Worker registration |
| `POST` | `/internal/v1/tasks/claim` | Claim an available task |
| `POST` | `/internal/v1/tasks/{id}/renew` | Renew a task lease |
| `POST` | `/internal/v1/tasks/{id}/complete` | Submit a successful result |
| `POST` | `/internal/v1/tasks/{id}/fail` | Report an execution failure |

Registration uses the bootstrap token. All subsequent Worker requests use the individual Worker token. Task lease operations additionally require the lease token issued when the task is claimed.

External Workers do not need to import Forge packages or implement internal Go interfaces. They only need to follow the HTTP protocol.

## Reference Go Worker

The reference implementation in `cmd/worker` demonstrates the complete Worker lifecycle.

It currently supports one task type:

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

This concurrency limit belongs to the reference implementation, not to the Forge HTTP protocol.
