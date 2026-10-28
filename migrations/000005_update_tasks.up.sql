ALTER TABLE tasks
    ADD COLUMN lease_owner      UUID        NULL,
    ADD CONSTRAINT fk_tasks_lease_owner FOREIGN KEY (lease_owner) REFERENCES workers (id) ON DELETE NO ACTION,
    ADD COLUMN lease_token      UUID        NULL,
    ADD COLUMN lease_expires_at TIMESTAMPTZ NULL;