ALTER TABLE tasks
    DROP CONSTRAINT IF EXISTS fk_tasks_lease_owner;

ALTER TABLE tasks
    DROP COLUMN IF EXISTS lease_owner,
    DROP COLUMN IF EXISTS lease_token,
    DROP COLUMN IF EXISTS lease_expires_at;