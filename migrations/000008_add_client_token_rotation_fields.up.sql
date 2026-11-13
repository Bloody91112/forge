ALTER TABLE clients
    ADD COLUMN previous_token_hash TEXT NULL,
    ADD COLUMN previous_token_expires_at TIMESTAMPTZ NULL;