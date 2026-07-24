BEGIN;

ALTER TABLE transfer.capture_resources
    ADD COLUMN IF NOT EXISTS source_type VARCHAR(32);

UPDATE transfer.capture_resources
SET source_type = 'postgresql'
WHERE source_type IS NULL;

ALTER TABLE transfer.capture_resources
    ALTER COLUMN source_type SET NOT NULL;

ALTER TABLE transfer.capture_resources
    DROP CONSTRAINT IF EXISTS chk_transfer_capture_source_type;

ALTER TABLE transfer.capture_resources
    ADD CONSTRAINT chk_transfer_capture_source_type
    CHECK (source_type IN ('postgresql', 'mysql'));

CREATE INDEX IF NOT EXISTS idx_transfer_capture_resources_source_type
    ON transfer.capture_resources (source_type);

CREATE TABLE IF NOT EXISTS transfer.postgresql_capture_resources (
    capture_resource_id BIGINT PRIMARY KEY,
    slot_name VARCHAR(63) NOT NULL,
    publication_name VARCHAR(63) NOT NULL,
    slot_owned BOOLEAN NOT NULL DEFAULT TRUE,
    publication_owned BOOLEAN NOT NULL DEFAULT TRUE,
    CONSTRAINT fk_transfer_capture_resources_postgre_sql
        FOREIGN KEY (capture_resource_id) REFERENCES transfer.capture_resources(id) ON DELETE CASCADE
);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'transfer' AND table_name = 'capture_resources' AND column_name = 'slot_name'
    ) THEN
        EXECUTE $migration$
            INSERT INTO transfer.postgresql_capture_resources (
                capture_resource_id, slot_name, publication_name, slot_owned, publication_owned
            )
            SELECT id, slot_name, publication_name, slot_owned, publication_owned
            FROM transfer.capture_resources
            WHERE source_type = 'postgresql'
            ON CONFLICT (capture_resource_id) DO NOTHING
        $migration$;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS transfer.mysql_capture_resources (
    capture_resource_id BIGINT PRIMARY KEY,
    connector_server_id BIGINT NOT NULL,
    schema_history_topic_name VARCHAR(255) NOT NULL,
    schema_history_topic_owned BOOLEAN NOT NULL DEFAULT TRUE,
    CONSTRAINT fk_transfer_capture_resources_my_sql
        FOREIGN KEY (capture_resource_id) REFERENCES transfer.capture_resources(id) ON DELETE CASCADE,
    CONSTRAINT chk_transfer_mysql_capture_server_id CHECK (connector_server_id BETWEEN 1 AND 4294967295)
);

ALTER TABLE transfer.mysql_capture_resources
    ADD COLUMN IF NOT EXISTS schema_history_topic_name VARCHAR(255),
    ADD COLUMN IF NOT EXISTS schema_history_topic_owned BOOLEAN NOT NULL DEFAULT TRUE;

UPDATE transfer.mysql_capture_resources m
SET schema_history_topic_name = '__addp_cdc_schema.' || c.tenant_id || '.' || c.task_id || '.' || c.generation
FROM transfer.capture_resources c
WHERE c.id = m.capture_resource_id
  AND (m.schema_history_topic_name IS NULL OR m.schema_history_topic_name = '');

ALTER TABLE transfer.mysql_capture_resources
    ALTER COLUMN schema_history_topic_name SET NOT NULL;

ALTER TABLE transfer.postgresql_capture_resources
    DROP CONSTRAINT IF EXISTS uq_transfer_postgresql_capture_slot,
    DROP CONSTRAINT IF EXISTS uq_transfer_postgresql_capture_publication;

CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_postgresql_capture_slot
    ON transfer.postgresql_capture_resources (slot_name);

CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_postgresql_capture_publication
    ON transfer.postgresql_capture_resources (publication_name);

ALTER TABLE transfer.mysql_capture_resources
    DROP CONSTRAINT IF EXISTS uq_transfer_mysql_capture_server_id;

CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_mysql_capture_server_id
    ON transfer.mysql_capture_resources (connector_server_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_mysql_capture_schema_history_topic
    ON transfer.mysql_capture_resources (schema_history_topic_name);

ALTER TABLE transfer.postgresql_capture_resources
    DROP CONSTRAINT IF EXISTS fk_transfer_capture_resources_postgre_sql;

ALTER TABLE transfer.postgresql_capture_resources
    ADD CONSTRAINT fk_transfer_capture_resources_postgre_sql
    FOREIGN KEY (capture_resource_id) REFERENCES transfer.capture_resources(id) ON DELETE CASCADE;

ALTER TABLE transfer.postgresql_capture_resources
    DROP CONSTRAINT IF EXISTS postgresql_capture_resources_capture_resource_id_fkey;

ALTER TABLE transfer.mysql_capture_resources
    DROP CONSTRAINT IF EXISTS fk_transfer_capture_resources_my_sql;

ALTER TABLE transfer.mysql_capture_resources
    ADD CONSTRAINT fk_transfer_capture_resources_my_sql
    FOREIGN KEY (capture_resource_id) REFERENCES transfer.capture_resources(id) ON DELETE CASCADE;

ALTER TABLE transfer.mysql_capture_resources
    DROP CONSTRAINT IF EXISTS mysql_capture_resources_capture_resource_id_fkey;

ALTER TABLE transfer.capture_resources
    DROP CONSTRAINT IF EXISTS uq_transfer_capture_slot,
    DROP CONSTRAINT IF EXISTS uq_transfer_capture_publication;

ALTER TABLE transfer.capture_resources
    DROP COLUMN IF EXISTS slot_name,
    DROP COLUMN IF EXISTS publication_name,
    DROP COLUMN IF EXISTS slot_owned,
    DROP COLUMN IF EXISTS publication_owned;

COMMIT;
