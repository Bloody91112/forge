
# Forge

Forge is a task processing system written in Go. It provides an HTTP API for submitting background tasks and distributes their execution across independent Worker processes.

Forge is designed to handle asynchronous workloads reliably: tasks are persisted, execution attempts are tracked, failed tasks can be retried, and abandoned tasks are recovered.

PostgreSQL is used as the primary storage and coordination mechanism. No separate message broker is required.

## Architecture

Forge consists of three components:

- **Forge API** — accepts tasks from clients, manages their lifecycle and coordinates Workers.
- **PostgreSQL** — stores tasks, execution attempts, client credentials, Worker registrations and system settings.
- **Worker** — independently registers with Forge, retrieves tasks, executes them and reports results.

```text
                 +----------------+
                 |     Client     |
                 +-------+--------+
                         |
                      HTTP API
                         |
                         v
                 +-------+--------+
                 |   Forge API    |
                 +-------+--------+
                         |
                   PostgreSQL
                         |
                 +-------+--------+
                 |                |
                 v                v
             +-------+        +-------+
             |Worker 1|        |Worker 2|
             +-------+        +-------+
```

Workers communicate with Forge exclusively through its HTTP API. They do not access PostgreSQL directly.

Multiple Worker processes can run independently to increase task processing capacity.

## Task processing

The typical processing flow:

1. A client submits a task to Forge.
2. Forge validates the request and saves the task in PostgreSQL.
3. An available Worker claims the task.
4. The Worker executes the task and reports either a result or an error.
5. Forge updates the task status and records the execution attempt.

Tasks have four possible states:

- `queued` — waiting for execution or retry.
- `running` — currently being processed.
- `succeeded` — completed successfully.
- `failed` — execution failed permanently or all attempts were exhausted.

Forge supports idempotent task creation, execution leases, automatic lease recovery and retries with exponential backoff.

Task delivery follows an at-least-once model. Task handlers must account for possible repeated execution.

## Reference Worker

The repository includes a reference Go Worker demonstrating the execution protocol.

Currently, it implements one task type: `sum-numbers`.

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

The reference Worker is an example implementation. Application-specific task handlers can be added independently of Forge API.

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

The setup script creates `.env` from `.env.example` when the file does not exist.

Before deployment, configure the database credentials and other required settings in `.env`.

Important variables:

| Variable | Description |
|---|---|
| `DB_DATABASE` | Main database name |
| `DB_TEST_DATABASE` | Test database name |
| `DB_USERNAME` | PostgreSQL username |
| `DB_PASSWORD` | PostgreSQL password |
| `DB_PORT` | PostgreSQL host port |
| `WORKER_CAPABILITIES` | Task types supported by the Worker |

For the reference Worker:

```dotenv
WORKER_CAPABILITIES=sum-numbers
```

Use different names for the main and test databases. Change the example database password before using the environment outside disposable local development.

### 3. Deploy Forge

Run the setup script:

```bash
chmod +x setup.sh
./setup.sh
```

The script performs the initial deployment:

1. Validates the environment and required tools.
2. Builds the Forge API and Worker images.
3. Starts PostgreSQL.
4. Applies database migrations to the main and test databases.
5. Initializes the Worker bootstrap token.
6. Starts Forge API and waits for readiness.
7. Starts the Worker and verifies its registration.

The API becomes available at:

`http://localhost:8080`

Readiness endpoint:

`GET /readyz`

## Using the API

All public API requests require a client Bearer token.

### 1. Create a client

Forge provides a CLI command for creating clients:

```text
forge client create "demo-client"
```

The command returns:

- Client ID
- Client name
- Client token

Save the token. It is displayed when created, while only its hash is stored in PostgreSQL.

### 2. Submit a task

Example request:

```http
POST /api/v1/tasks
Authorization: Bearer <client-token>
Content-Type: application/json
Idempotency-Key: sum-example-001
```

Request body:

```json
{
  "type": "sum-numbers",
  "payload": {
    "number_1": 19,
    "number_2": 23
  }
}
```

A successfully created task returns HTTP `202` with its ID.

The `Idempotency-Key` header is mandatory. Repeating the same request with the same key returns the existing task instead of creating a duplicate.

### 3. Retrieve a task

```http
GET /api/v1/tasks/{task-id}
Authorization: Bearer <client-token>
```

The response contains the current task status, execution attempt count, result or error information.

### 4. Retrieve execution history

```http
GET /api/v1/tasks/{task-id}/attempts
Authorization: Bearer <client-token>
```

The response contains the history of execution attempts, including their outcomes and errors.

## API reference

| Method | Endpoint | Description |
|---|---|---|
| POST | `/api/v1/tasks` | Create a task |
| GET | `/api/v1/tasks/{id}` | Retrieve task status and result |
| GET | `/api/v1/tasks/{id}/attempts` | Retrieve execution history |
| GET | `/healthz` | Check API process liveness |
| GET | `/readyz` | Check API readiness |

Worker communication uses separate internal endpoints under `/internal/v1/`. These endpoints are intended for Workers, not API clients.

## Extending Workers

To support a new task type:

1. Implement the `ForgeTaskHandler` interface.
2. Register the handler in `cmd/worker/main.go`.
3. Add the task type to `WORKER_CAPABILITIES`.
4. Deploy Workers with the updated implementation.

Forge does not contain application-specific business handlers. Task execution logic belongs to Worker implementations.

## Client credentials

Client tokens can be rotated using the Forge CLI:

```text
forge client rotate <client-id>
```

The previous token remains valid for five minutes after rotation.

Worker registration uses a separate bootstrap token, while registered Workers authenticate using their individual Worker tokens.

## License

No license has been specified yet.
