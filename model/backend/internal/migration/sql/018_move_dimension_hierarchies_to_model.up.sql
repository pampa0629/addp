-- Dimension hierarchies are Model-owned aggregate children of one dimension table.
-- The former field-level Standard reference encoded the same fact twice and is removed.
ALTER TABLE model.logical_fields
    DROP CONSTRAINT IF EXISTS ck_model_logical_field_hierarchy,
    DROP CONSTRAINT IF EXISTS ck_model_logical_field_hierarchy_id,
    DROP COLUMN IF EXISTS hierarchy_id,
    DROP COLUMN IF EXISTS hierarchy_level;

DELETE FROM model.standard_reference_guards WHERE resource_type = 'dimension_hierarchy';
ALTER TABLE model.standard_reference_guards
    DROP CONSTRAINT ck_model_standard_reference_guard_resource_type,
    ADD CONSTRAINT ck_model_standard_reference_guard_resource_type
        CHECK (resource_type IN ('domain', 'element', 'metric'));

CREATE TABLE model.dimension_hierarchies (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    table_id BIGINT NOT NULL,
    name VARCHAR(200) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_model_dimension_hierarchy_id_table UNIQUE (id, table_id),
    CONSTRAINT uq_model_dimension_hierarchy_table_name UNIQUE (table_id, name),
    CONSTRAINT fk_model_dimension_hierarchy_table
        FOREIGN KEY (table_id, tenant_id)
        REFERENCES model.logical_tables(id, tenant_id)
        ON DELETE CASCADE
);

CREATE TABLE model.dimension_hierarchy_levels (
    id BIGSERIAL PRIMARY KEY,
    hierarchy_id BIGINT NOT NULL,
    field_id BIGINT NOT NULL,
    level_num INTEGER NOT NULL,
    level_name VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_model_dimension_hierarchy_level_num CHECK (level_num > 0),
    CONSTRAINT uq_model_dimension_hierarchy_level_num UNIQUE (hierarchy_id, level_num),
    CONSTRAINT uq_model_dimension_hierarchy_level_field UNIQUE (hierarchy_id, field_id),
    CONSTRAINT fk_model_dimension_hierarchy_level_hierarchy
        FOREIGN KEY (hierarchy_id)
        REFERENCES model.dimension_hierarchies(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_model_dimension_hierarchy_level_field
        FOREIGN KEY (field_id)
        REFERENCES model.logical_fields(id)
        ON DELETE RESTRICT
);

CREATE INDEX idx_model_dimension_hierarchies_tenant_table
    ON model.dimension_hierarchies(tenant_id, table_id, id);
CREATE INDEX idx_model_dimension_hierarchy_levels_hierarchy_order
    ON model.dimension_hierarchy_levels(hierarchy_id, level_num, id);
