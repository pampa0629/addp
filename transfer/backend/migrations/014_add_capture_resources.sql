CREATE TABLE IF NOT EXISTS transfer.capture_resources (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL,
    tenant_id BIGINT NOT NULL,
    generation BIGINT NOT NULL,
    connector_name VARCHAR(255) NOT NULL,
    topic_name VARCHAR(255) NOT NULL,
    consumer_group VARCHAR(255) NOT NULL,
    slot_name VARCHAR(63) NOT NULL,
    publication_name VARCHAR(63) NOT NULL,
    source_identity TEXT NOT NULL,
    source_connection_fingerprint VARCHAR(64) NOT NULL,
    source_engine_id BIGINT NOT NULL,
    source_database VARCHAR(255) NOT NULL,
    source_schema VARCHAR(255) NOT NULL,
    source_table VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL,
    connector_status VARCHAR(32) NOT NULL DEFAULT '',
    connector_error TEXT NOT NULL DEFAULT '',
    topic_created BOOLEAN NOT NULL DEFAULT FALSE,
    connector_created BOOLEAN NOT NULL DEFAULT FALSE,
    slot_owned BOOLEAN NOT NULL DEFAULT TRUE,
    publication_owned BOOLEAN NOT NULL DEFAULT TRUE,
    resource_version BIGINT NOT NULL DEFAULT 1,
    last_observed_at TIMESTAMPTZ,
    stopped_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_transfer_capture_generation UNIQUE (task_id, generation),
    CONSTRAINT uq_transfer_capture_connector UNIQUE (connector_name),
    CONSTRAINT uq_transfer_capture_topic UNIQUE (topic_name),
    CONSTRAINT uq_transfer_capture_group UNIQUE (consumer_group),
    CONSTRAINT uq_transfer_capture_slot UNIQUE (slot_name),
    CONSTRAINT uq_transfer_capture_publication UNIQUE (publication_name),
    CONSTRAINT chk_transfer_capture_status CHECK (status IN ('provisioning', 'running', 'failed', 'cleaning', 'cleanup_failed', 'stopped'))
);

CREATE INDEX IF NOT EXISTS idx_transfer_capture_resources_tenant
    ON transfer.capture_resources (tenant_id);

CREATE INDEX IF NOT EXISTS idx_transfer_capture_resources_status
    ON transfer.capture_resources (status);
