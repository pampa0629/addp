CREATE TABLE IF NOT EXISTS transfer.sync_states (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL,
    source_identity TEXT NOT NULL,
    partition VARCHAR(255) NOT NULL DEFAULT 'default',
    position JSONB,
    position_type VARCHAR(50) NOT NULL DEFAULT 'watermark',
    position_version VARCHAR(20) NOT NULL DEFAULT 'v1',
    state_version BIGINT NOT NULL DEFAULT 0,
    fencing_token BIGINT NOT NULL DEFAULT 0,
    updated_execution_id VARCHAR(36),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_transfer_sync_state_identity UNIQUE (task_id, source_identity, partition)
);

CREATE INDEX IF NOT EXISTS idx_transfer_sync_states_task_id
    ON transfer.sync_states (task_id);
