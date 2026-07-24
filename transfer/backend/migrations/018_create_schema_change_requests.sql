ALTER TABLE transfer.capture_resources
    ADD COLUMN IF NOT EXISTS schema_revision BIGINT NOT NULL DEFAULT 1
    CHECK (schema_revision >= 1);

CREATE TABLE IF NOT EXISTS transfer.schema_change_requests (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL,
    tenant_id BIGINT NOT NULL,
    capture_resource_id BIGINT NOT NULL,
    generation BIGINT NOT NULL,
    execution_id VARCHAR(36) NOT NULL,
    source_partition VARCHAR(255) NOT NULL,
    source_offset BIGINT NOT NULL CHECK (source_offset >= 0),
    scope VARCHAR(255) NOT NULL,
    diff JSONB NOT NULL,
    approved_mappings JSONB NOT NULL DEFAULT '{}'::jsonb,
    from_revision BIGINT NOT NULL CHECK (from_revision >= 1),
    to_revision BIGINT NOT NULL CHECK (to_revision = from_revision + 1),
    status VARCHAR(20) NOT NULL CHECK (status IN ('pending', 'applied')),
    applied_by BIGINT,
    detected_at TIMESTAMPTZ NOT NULL,
    applied_at TIMESTAMPTZ,
    metadata_scan_status VARCHAR(20) NOT NULL DEFAULT '',
    metadata_scan_execution_id VARCHAR(36) NOT NULL DEFAULT '',
    metadata_scan_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_transfer_schema_change_capture
        FOREIGN KEY (capture_resource_id) REFERENCES transfer.capture_resources(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_transfer_schema_change_task
    ON transfer.schema_change_requests (tenant_id, task_id, detected_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_schema_change_pending_generation
    ON transfer.schema_change_requests (capture_resource_id)
    WHERE status = 'pending';

CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_schema_change_revision
    ON transfer.schema_change_requests (capture_resource_id, to_revision);
