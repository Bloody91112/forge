CREATE TABLE tasks
(
    id              UUID PRIMARY KEY,
    client_id       UUID        NOT NULL REFERENCES clients (id),
    idempotency_key TEXT        NOT NULL,
    request_hash    TEXT        NOT NULL,
    type            TEXT        NOT NULL,
    payload         JSONB       NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    result          JSONB       NULL,
    error_code      TEXT        NULL,
    error_message   TEXT        NULL,
    attempt_count   BIGINT      NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts    BIGINT      NOT NULL DEFAULT 5 CHECK (max_attempts >= 1),
    timeout_seconds BIGINT      NOT NULL DEFAULT 300 CHECK (timeout_seconds >= 1),
    created_at      TIMESTAMPTZ          DEFAULT CURRENT_TIMESTAMP NOT NULL,
    available_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at      TIMESTAMPTZ NULL,
    finished_at     TIMESTAMPTZ NULL,

    UNIQUE (client_id, idempotency_key)
);

CREATE INDEX idx_tasks_claim
    ON tasks (type, available_at, created_at)
    WHERE status = 'queued';