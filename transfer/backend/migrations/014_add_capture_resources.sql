CREATE TABLE IF NOT EXISTS transfer.capture_resources (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL,
    tenant_id BIGINT NOT NULL,
    generation BIGINT NOT NULL,
    connector_name VARCHAR(255) NOT NULL,
    topic_name VARCHAR(255) NOT NULL,
    consumer_group VARCHAR(255) NOT NULL,
    source_type VARCHAR(32) NOT NULL,
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
    resource_version BIGINT NOT NULL DEFAULT 1,
    last_observed_at TIMESTAMPTZ,
    stopped_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_transfer_capture_generation UNIQUE (task_id, generation),
    CONSTRAINT uq_transfer_capture_connector UNIQUE (connector_name),
    CONSTRAINT uq_transfer_capture_topic UNIQUE (topic_name),
    CONSTRAINT uq_transfer_capture_group UNIQUE (consumer_group),
    CONSTRAINT chk_transfer_capture_source_type CHECK (source_type IN ('postgresql', 'mysql')),
    CONSTRAINT chk_transfer_capture_status CHECK (status IN ('provisioning', 'running', 'failed', 'cleaning', 'cleanup_failed', 'stopped'))
);

CREATE TABLE IF NOT EXISTS transfer.postgresql_capture_resources (
    capture_resource_id BIGINT PRIMARY KEY,
    slot_name VARCHAR(63) NOT NULL,
    publication_name VARCHAR(63) NOT NULL,
    slot_owned BOOLEAN NOT NULL DEFAULT TRUE,
    publication_owned BOOLEAN NOT NULL DEFAULT TRUE,
    CONSTRAINT fk_transfer_capture_resources_postgre_sql
        FOREIGN KEY (capture_resource_id) REFERENCES transfer.capture_resources(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_postgresql_capture_slot
    ON transfer.postgresql_capture_resources (slot_name);

CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_postgresql_capture_publication
    ON transfer.postgresql_capture_resources (publication_name);

CREATE TABLE IF NOT EXISTS transfer.mysql_capture_resources (
    capture_resource_id BIGINT PRIMARY KEY,
    connector_server_id BIGINT NOT NULL,
    schema_history_topic_name VARCHAR(255) NOT NULL,
    schema_history_topic_owned BOOLEAN NOT NULL DEFAULT TRUE,
    CONSTRAINT fk_transfer_capture_resources_my_sql
        FOREIGN KEY (capture_resource_id) REFERENCES transfer.capture_resources(id) ON DELETE CASCADE,
    CONSTRAINT chk_transfer_mysql_capture_server_id CHECK (connector_server_id BETWEEN 1 AND 4294967295)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_mysql_capture_server_id
    ON transfer.mysql_capture_resources (connector_server_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_mysql_capture_schema_history_topic
    ON transfer.mysql_capture_resources (schema_history_topic_name);

CREATE INDEX IF NOT EXISTS idx_transfer_capture_resources_tenant
    ON transfer.capture_resources (tenant_id);

CREATE INDEX IF NOT EXISTS idx_transfer_capture_resources_status
    ON transfer.capture_resources (status);

CREATE INDEX IF NOT EXISTS idx_transfer_capture_resources_source_type
    ON transfer.capture_resources (source_type);
