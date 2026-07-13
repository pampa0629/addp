BEGIN;

ALTER TABLE transfer.transfer_tasks
    ADD COLUMN IF NOT EXISTS desired_state VARCHAR(20) NOT NULL DEFAULT 'stopped';

ALTER TABLE transfer.transfer_tasks
    DROP CONSTRAINT IF EXISTS chk_transfer_tasks_desired_state;

ALTER TABLE transfer.transfer_tasks
    ADD CONSTRAINT chk_transfer_tasks_desired_state
    CHECK (desired_state IN ('running', 'paused', 'stopped'));

CREATE INDEX IF NOT EXISTS idx_transfer_tasks_desired_state
    ON transfer.transfer_tasks (desired_state);

CREATE TABLE IF NOT EXISTS transfer.runtime_leases (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL,
    execution_id VARCHAR(255) NOT NULL,
    owner_instance_id VARCHAR(255) NOT NULL,
    lease_until TIMESTAMPTZ NOT NULL,
    heartbeat_at TIMESTAMPTZ NOT NULL,
    fencing_token BIGINT NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_transfer_runtime_leases_task UNIQUE (task_id),
    CONSTRAINT uq_transfer_runtime_leases_execution UNIQUE (execution_id)
);

CREATE INDEX IF NOT EXISTS idx_transfer_runtime_leases_lease_until
    ON transfer.runtime_leases (lease_until);

CREATE INDEX IF NOT EXISTS idx_transfer_runtime_leases_owner
    ON transfer.runtime_leases (owner_instance_id);

COMMIT;
