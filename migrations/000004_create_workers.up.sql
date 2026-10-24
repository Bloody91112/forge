CREATE TABLE workers
(
    id               UUID PRIMARY KEY,
    token_hash       TEXT        NOT NULL,
    protocol_version TEXT        NOT NULL DEFAULT '1',
    capabilities     JSONB       NOT NULL,
    registered_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at     TIMESTAMPTZ NULL,
    lease_expires_at TIMESTAMPTZ NOT NULL
);