CREATE INDEX idx_tasks_expired_lease
    ON tasks (lease_expires_at)
    WHERE status = 'running';