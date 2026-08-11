BEGIN;

ALTER TABLE transfer.capture_resources
    DROP CONSTRAINT IF EXISTS chk_transfer_capture_source_type;

ALTER TABLE transfer.capture_resources
    ADD CONSTRAINT chk_transfer_capture_source_type
    CHECK (source_type IN ('postgresql', 'mysql', 'oracle'));

CREATE TABLE IF NOT EXISTS transfer.oracle_capture_resources (
    capture_resource_id BIGINT PRIMARY KEY,
    schema_history_topic_name VARCHAR(255) NOT NULL,
    schema_history_topic_owned BOOLEAN NOT NULL DEFAULT TRUE,
    CONSTRAINT fk_transfer_capture_resources_oracle
        FOREIGN KEY (capture_resource_id) REFERENCES transfer.capture_resources(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_oracle_capture_schema_history_topic
    ON transfer.oracle_capture_resources (schema_history_topic_name);

COMMIT;
