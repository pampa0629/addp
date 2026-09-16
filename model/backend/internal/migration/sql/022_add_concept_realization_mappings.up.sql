CREATE UNIQUE INDEX IF NOT EXISTS uq_model_entity_attributes_id_entity
    ON model.entity_attributes(id, entity_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_model_entity_relations_id_tenant
    ON model.entity_relations(id, tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_model_table_relations_id_source_tenant
    ON model.table_relations(id, source_table, tenant_id);

CREATE TABLE model.logical_table_entity_mappings (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    table_id BIGINT NOT NULL,
    entity_id BIGINT NOT NULL,
    mapping_role VARCHAR(24) NOT NULL,
    entity_version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_model_table_entity_mapping_role CHECK (mapping_role IN ('represents', 'derives_from')),
    CONSTRAINT ck_model_table_entity_mapping_version CHECK (entity_version > 0),
    CONSTRAINT fk_model_table_entity_mapping_table
        FOREIGN KEY (table_id, tenant_id) REFERENCES model.logical_tables(id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT fk_model_table_entity_mapping_entity
        FOREIGN KEY (entity_id, tenant_id) REFERENCES model.entities(id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT uq_model_table_entity_mapping UNIQUE (table_id, entity_id),
    CONSTRAINT uq_model_table_entity_mapping_tenant UNIQUE (table_id, entity_id, tenant_id)
);

CREATE TABLE model.logical_field_attribute_mappings (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    table_id BIGINT NOT NULL,
    field_id BIGINT NOT NULL,
    entity_id BIGINT NOT NULL,
    entity_attribute_id BIGINT NOT NULL,
    mapping_role VARCHAR(16) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_model_field_attribute_mapping_role CHECK (mapping_role IN ('direct', 'derived')),
    CONSTRAINT fk_model_field_attribute_mapping_field
        FOREIGN KEY (field_id, table_id) REFERENCES model.logical_fields(id, table_id) ON DELETE CASCADE,
    CONSTRAINT fk_model_field_attribute_mapping_entity
        FOREIGN KEY (table_id, entity_id, tenant_id)
        REFERENCES model.logical_table_entity_mappings(table_id, entity_id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT fk_model_field_attribute_mapping_attribute
        FOREIGN KEY (entity_attribute_id, entity_id)
        REFERENCES model.entity_attributes(id, entity_id) ON DELETE RESTRICT,
    CONSTRAINT uq_model_field_attribute_mapping UNIQUE (field_id, entity_attribute_id)
);

CREATE TABLE model.table_relation_entity_relation_mappings (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    table_id BIGINT NOT NULL,
    table_relation_id BIGINT NOT NULL,
    entity_relation_id BIGINT NOT NULL,
    orientation VARCHAR(16) NOT NULL,
    entity_relation_version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_model_relation_mapping_orientation CHECK (orientation IN ('same', 'inverse')),
    CONSTRAINT ck_model_relation_mapping_version CHECK (entity_relation_version > 0),
    CONSTRAINT fk_model_relation_mapping_table_relation
        FOREIGN KEY (table_relation_id, table_id, tenant_id)
        REFERENCES model.table_relations(id, source_table, tenant_id) ON DELETE CASCADE,
    CONSTRAINT fk_model_relation_mapping_entity_relation
        FOREIGN KEY (entity_relation_id, tenant_id)
        REFERENCES model.entity_relations(id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT uq_model_relation_mapping UNIQUE (table_relation_id, entity_relation_id)
);

CREATE INDEX idx_model_table_entity_mappings_entity
    ON model.logical_table_entity_mappings(tenant_id, entity_id, table_id);
CREATE INDEX idx_model_field_attribute_mappings_attribute
    ON model.logical_field_attribute_mappings(tenant_id, entity_attribute_id, field_id);
CREATE INDEX idx_model_relation_mappings_entity_relation
    ON model.table_relation_entity_relation_mappings(tenant_id, entity_relation_id, table_relation_id);

INSERT INTO model.logical_table_entity_mappings (
    tenant_id, table_id, entity_id, mapping_role, entity_version
)
SELECT logical_table.tenant_id, logical_table.id, entity.id, 'represents', entity.version
FROM model.logical_tables AS logical_table
JOIN model.entities AS entity
  ON entity.id = logical_table.entity_id AND entity.tenant_id = logical_table.tenant_id
WHERE logical_table.entity_id IS NOT NULL;

ALTER TABLE model.logical_tables DROP CONSTRAINT IF EXISTS fk_model_logical_table_entity;
ALTER TABLE model.logical_tables DROP CONSTRAINT IF EXISTS ck_model_logical_table_entity_id;
ALTER TABLE model.logical_tables DROP COLUMN entity_id;

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
            'domain_id', CASE WHEN changed.domain_id IS NULL THEN NULL ELSE changed.domain_id::TEXT END
        )),
        NOW()
    );
    RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
END;
$$;
