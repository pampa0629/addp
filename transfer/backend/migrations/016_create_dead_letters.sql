CREATE TABLE IF NOT EXISTS transfer.dead_letters (
    identity UUID PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    task_id BIGINT NOT NULL,
    apply_identity UUID NOT NULL,
    first_execution_id VARCHAR(255) NOT NULL,
    last_execution_id VARCHAR(255) NOT NULL,
    source_identity TEXT NOT NULL,
    source_topic VARCHAR(249) NOT NULL,
    source_partition VARCHAR(255) NOT NULL,
    source_offset BIGINT NOT NULL,
    source_timestamp TIMESTAMPTZ,
    error_code VARCHAR(128) NOT NULL,
    error_category VARCHAR(128) NOT NULL,
    error_message TEXT NOT NULL,
    payload_topic VARCHAR(249) NOT NULL,
    payload_partition INTEGER NOT NULL,
    payload_offset BIGINT NOT NULL,
    payload_available BOOLEAN NOT NULL DEFAULT TRUE,
    first_observed_at TIMESTAMPTZ NOT NULL,
    last_observed_at TIMESTAMPTZ NOT NULL,
    occurrence_count BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_transfer_dead_letters_source_record
        UNIQUE (apply_identity, source_identity, source_partition, source_offset),
    CONSTRAINT chk_transfer_dead_letters_source_offset
        CHECK (source_offset >= 0),
    CONSTRAINT chk_transfer_dead_letters_payload_partition
        CHECK (payload_partition >= 0),
    CONSTRAINT chk_transfer_dead_letters_payload_offset
        CHECK (payload_offset >= 0),
    CONSTRAINT chk_transfer_dead_letters_occurrence_count
        CHECK (occurrence_count > 0)
);

CREATE INDEX IF NOT EXISTS idx_transfer_dead_letters_task_observed
    ON transfer.dead_letters (tenant_id, task_id, last_observed_at DESC);

CREATE INDEX IF NOT EXISTS idx_transfer_dead_letters_error
    ON transfer.dead_letters (tenant_id, error_category, error_code);
