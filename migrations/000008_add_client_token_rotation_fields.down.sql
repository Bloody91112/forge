ALTER TABLE clients
    DROP COLUMN IF EXISTS previous_token_hash,
    DROP COLUMN IF EXISTS previous_token_expires_at;