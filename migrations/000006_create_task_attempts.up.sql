CREATE TABLE task_attempts
(
    id             UUID PRIMARY KEY,
    task_id        UUID        NOT NULL,
    attempt_number INTEGER     NOT NULL DEFAULT 1 CHECK (attempt_number >= 1),
    worker_id      UUID        NOT NULL,
    started_at     TIMESTAMPTZ NOT NULL,
    finished_at    TIMESTAMPTZ NULL,
    outcome        TEXT        NULL CHECK (outcome IN ('succeeded', 'retryable_error', 'permanent_error', 'lease_expired')),
    error_code     TEXT        NULL,
    error_message  TEXT        NULL,

    CONSTRAINT fk_task_attempts_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE NO ACTION,
    CONSTRAINT fk_task_attempts_worker FOREIGN KEY (worker_id) REFERENCES workers (id) ON DELETE NO ACTION
);