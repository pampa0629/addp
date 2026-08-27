CREATE TABLE model.catalog_resource_changes (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    source_type VARCHAR(32) NOT NULL,
    source_identity BIGINT NOT NULL,
    operation VARCHAR(16) NOT NULL,
    resource_version BIGINT NOT NULL,
    snapshot JSONB NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_model_catalog_change_source_type CHECK (source_type IN ('entity', 'logical_table')),
    CONSTRAINT ck_model_catalog_change_operation CHECK (operation IN ('upsert', 'missing')),
    CONSTRAINT ck_model_catalog_change_identity CHECK (source_identity > 0 AND resource_version > 0)
);

CREATE INDEX idx_model_catalog_changes_tenant_cursor
    ON model.catalog_resource_changes (tenant_id, id);
CREATE INDEX idx_model_catalog_changes_source
    ON model.catalog_resource_changes (tenant_id, source_type, source_identity, id DESC);

INSERT INTO model.catalog_resource_changes (
    tenant_id, source_type, source_identity, operation, resource_version, snapshot, observed_at
)
SELECT
    entity.tenant_id,
    'entity',
    entity.id,
    'upsert',
    entity.version,
    jsonb_strip_nulls(jsonb_build_object(
        'name', entity.name,
        'code', entity.code,
        'object_kind', 'entity',
        'model_status', entity.status,
        'domain_id', CASE WHEN entity.domain_id IS NULL THEN NULL ELSE entity.domain_id::TEXT END
    )),
    COALESCE(entity.updated_at, entity.created_at, NOW())
FROM model.entities AS entity
ORDER BY entity.id;

INSERT INTO model.catalog_resource_changes (
    tenant_id, source_type, source_identity, operation, resource_version, snapshot, observed_at
)
SELECT
    logical_table.tenant_id,
    'logical_table',
    logical_table.id,
    'upsert',
    logical_table.version,
    jsonb_strip_nulls(jsonb_build_object(
        'name', logical_table.name,
        'code', logical_table.code,
        'object_kind', 'logical_table',
        'model_status', logical_table.status,
        'table_type', logical_table.table_type,
        'layer', logical_table.layer,
        'domain_id', CASE WHEN logical_table.domain_id IS NULL THEN NULL ELSE logical_table.domain_id::TEXT END,
        'entity_id', CASE WHEN logical_table.entity_id IS NULL THEN NULL ELSE logical_table.entity_id::TEXT END
    )),
    COALESCE(logical_table.updated_at, logical_table.created_at, NOW())
FROM model.logical_tables AS logical_table
ORDER BY logical_table.id;

CREATE OR REPLACE FUNCTION model.capture_entity_catalog_change()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    changed model.entities%ROWTYPE;
BEGIN
    changed := CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
    INSERT INTO model.catalog_resource_changes (
        tenant_id, source_type, source_identity, operation, resource_version, snapshot, observed_at
    ) VALUES (
        changed.tenant_id,
        'entity',
        changed.id,
        CASE WHEN TG_OP = 'DELETE' THEN 'missing' ELSE 'upsert' END,
        changed.version,
        jsonb_strip_nulls(jsonb_build_object(
            'name', changed.name,
            'code', changed.code,
            'object_kind', 'entity',
            'model_status', changed.status,
            'domain_id', CASE WHEN changed.domain_id IS NULL THEN NULL ELSE changed.domain_id::TEXT END
        )),
        NOW()
    );
    RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
END;
$$;

CREATE OR REPLACE FUNCTION model.capture_logical_table_catalog_change()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    changed model.logical_tables%ROWTYPE;
BEGIN
    changed := CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
    INSERT INTO model.catalog_resource_changes (
        tenant_id, source_type, source_identity, operation, resource_version, snapshot, observed_at
    ) VALUES (
        changed.tenant_id,
        'logical_table',
        changed.id,
        CASE WHEN TG_OP = 'DELETE' THEN 'missing' ELSE 'upsert' END,
        changed.version,
        jsonb_strip_nulls(jsonb_build_object(
            'name', changed.name,
            'code', changed.code,
            'object_kind', 'logical_table',
            'model_status', changed.status,
            'table_type', changed.table_type,
            'layer', changed.layer,
            'domain_id', CASE WHEN changed.domain_id IS NULL THEN NULL ELSE changed.domain_id::TEXT END,
            'entity_id', CASE WHEN changed.entity_id IS NULL THEN NULL ELSE changed.entity_id::TEXT END
        )),
        NOW()
    );
    RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
END;
$$;

CREATE TRIGGER trg_model_entity_catalog_change
AFTER INSERT OR UPDATE OR DELETE ON model.entities
FOR EACH ROW EXECUTE FUNCTION model.capture_entity_catalog_change();

CREATE TRIGGER trg_model_logical_table_catalog_change
AFTER INSERT OR UPDATE OR DELETE ON model.logical_tables
FOR EACH ROW EXECUTE FUNCTION model.capture_logical_table_catalog_change();
