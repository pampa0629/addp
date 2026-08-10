CREATE TABLE IF NOT EXISTS model.dw_layers (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    layer_code VARCHAR(20) NOT NULL,
    layer_name VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    naming_rule TEXT NOT NULL DEFAULT '',
    quality_sla JSONB,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS model.entities (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    domain_id BIGINT,
    name VARCHAR(200) NOT NULL,
    code VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'draft',
    created_by BIGINT NOT NULL,
    updated_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS model.entity_attributes (
    id BIGSERIAL PRIMARY KEY,
    entity_id BIGINT NOT NULL,
    element_id BIGINT,
    name VARCHAR(200) NOT NULL,
    column_name VARCHAR(200) NOT NULL,
    data_type VARCHAR(50) NOT NULL,
    is_pk BOOLEAN NOT NULL DEFAULT FALSE,
    nullable BOOLEAN NOT NULL DEFAULT TRUE,
    description TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS model.entity_relations (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    source_entity BIGINT NOT NULL,
    target_entity BIGINT NOT NULL,
    relation_type VARCHAR(20) NOT NULL,
    name VARCHAR(200) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS model.logical_tables (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    domain_id BIGINT,
    entity_id BIGINT,
    name VARCHAR(200) NOT NULL,
    code VARCHAR(200) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    table_type VARCHAR(30) NOT NULL,
    layer VARCHAR(20),
    status VARCHAR(20) NOT NULL DEFAULT 'draft',
    grain_description TEXT NOT NULL DEFAULT '',
    scd_type INTEGER NOT NULL DEFAULT 0,
    materialization JSONB,
    created_by BIGINT NOT NULL,
    updated_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS model.logical_fields (
    id BIGSERIAL PRIMARY KEY,
    table_id BIGINT NOT NULL,
    element_id BIGINT,
    name VARCHAR(200) NOT NULL,
    column_name VARCHAR(200) NOT NULL,
    data_type VARCHAR(50) NOT NULL,
    length INTEGER,
    nullable BOOLEAN NOT NULL DEFAULT TRUE,
    is_pk BOOLEAN NOT NULL DEFAULT FALSE,
    is_partition BOOLEAN NOT NULL DEFAULT FALSE,
    default_value TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    field_role VARCHAR(30) NOT NULL DEFAULT 'regular',
    hierarchy_id BIGINT,
    hierarchy_level INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS model.table_relations (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    source_table BIGINT NOT NULL,
    source_field BIGINT NOT NULL,
    target_table BIGINT NOT NULL,
    target_field BIGINT NOT NULL,
    relation_type VARCHAR(20) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS model.fact_metric_mappings (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    fact_table_id BIGINT NOT NULL,
    metric_id BIGINT NOT NULL,
    field_id BIGINT,
    note TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE model.entity_attributes ADD COLUMN IF NOT EXISTS column_name VARCHAR(200);
ALTER TABLE model.entity_attributes ADD COLUMN IF NOT EXISTS data_type VARCHAR(50);
UPDATE model.entity_attributes
SET column_name = 'attribute_' || id
WHERE column_name IS NULL OR btrim(column_name) = '';
UPDATE model.entity_attributes
SET data_type = 'string'
WHERE data_type IS NULL OR btrim(data_type) = '';
ALTER TABLE model.entity_attributes ALTER COLUMN column_name SET NOT NULL;
ALTER TABLE model.entity_attributes ALTER COLUMN data_type SET NOT NULL;

-- 收敛旧实现遗留值，再建立正式约束。
UPDATE model.logical_tables SET status = 'approved' WHERE status = 'materialized';
UPDATE model.logical_tables SET layer = NULL WHERE layer IS NOT NULL AND btrim(layer) = '';

CREATE UNIQUE INDEX uq_model_dw_layers_tenant_code ON model.dw_layers(tenant_id, layer_code);
CREATE UNIQUE INDEX uq_model_dw_layers_id_tenant ON model.dw_layers(id, tenant_id);
CREATE UNIQUE INDEX uq_model_entities_tenant_code ON model.entities(tenant_id, code);
CREATE UNIQUE INDEX uq_model_entities_id_tenant ON model.entities(id, tenant_id);
CREATE UNIQUE INDEX uq_model_entity_attributes_entity_column ON model.entity_attributes(entity_id, column_name);
CREATE UNIQUE INDEX uq_model_entity_relations_identity ON model.entity_relations(tenant_id, source_entity, target_entity, relation_type, name);
CREATE UNIQUE INDEX uq_model_logical_tables_tenant_code ON model.logical_tables(tenant_id, code);
CREATE UNIQUE INDEX uq_model_logical_tables_id_tenant ON model.logical_tables(id, tenant_id);
CREATE UNIQUE INDEX uq_model_logical_fields_table_column ON model.logical_fields(table_id, column_name);
CREATE UNIQUE INDEX uq_model_logical_fields_id_table ON model.logical_fields(id, table_id);
CREATE UNIQUE INDEX uq_model_table_relations_identity ON model.table_relations(tenant_id, source_table, source_field, target_table, target_field);
CREATE UNIQUE INDEX uq_model_fact_metric_identity ON model.fact_metric_mappings(tenant_id, fact_table_id, metric_id);

ALTER TABLE model.dw_layers
    ADD CONSTRAINT ck_model_dw_layer_code CHECK (layer_code ~ '^[a-z][a-z0-9_]*$'),
    ADD CONSTRAINT ck_model_dw_layer_sort_order CHECK (sort_order >= 0);
ALTER TABLE model.entities
    ADD CONSTRAINT ck_model_entity_code CHECK (code ~ '^[a-z][a-z0-9_]*$'),
    ADD CONSTRAINT ck_model_entity_status CHECK (status IN ('draft', 'approved'));
ALTER TABLE model.entity_attributes
    ADD CONSTRAINT ck_model_entity_attribute_column CHECK (column_name ~ '^[a-z][a-z0-9_]*$'),
    ADD CONSTRAINT ck_model_entity_attribute_type CHECK (data_type IN ('string', 'int', 'bigint', 'float', 'decimal', 'date', 'datetime', 'bool', 'json', 'text', 'geometry')),
    ADD CONSTRAINT fk_model_entity_attribute_entity FOREIGN KEY (entity_id) REFERENCES model.entities(id) ON DELETE CASCADE;
ALTER TABLE model.entity_relations
    ADD CONSTRAINT ck_model_entity_relation_distinct CHECK (source_entity <> target_entity),
    ADD CONSTRAINT ck_model_entity_relation_type CHECK (relation_type IN ('one_to_one', 'one_to_many', 'many_to_many')),
    ADD CONSTRAINT fk_model_entity_relation_source FOREIGN KEY (source_entity, tenant_id) REFERENCES model.entities(id, tenant_id) ON DELETE CASCADE,
    ADD CONSTRAINT fk_model_entity_relation_target FOREIGN KEY (target_entity, tenant_id) REFERENCES model.entities(id, tenant_id) ON DELETE CASCADE;
ALTER TABLE model.logical_tables
    ADD CONSTRAINT ck_model_logical_table_code CHECK (code ~ '^[a-z][a-z0-9_]*$'),
    ADD CONSTRAINT ck_model_logical_table_type CHECK (table_type IN ('entity', 'fact', 'dimension')),
    ADD CONSTRAINT ck_model_logical_table_status CHECK (status IN ('draft', 'approved')),
    ADD CONSTRAINT ck_model_logical_table_scd CHECK ((table_type = 'dimension' AND scd_type BETWEEN 0 AND 3) OR (table_type <> 'dimension' AND scd_type = 0)),
    ADD CONSTRAINT fk_model_logical_table_layer FOREIGN KEY (tenant_id, layer) REFERENCES model.dw_layers(tenant_id, layer_code) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_model_logical_table_entity FOREIGN KEY (entity_id, tenant_id) REFERENCES model.entities(id, tenant_id) ON DELETE RESTRICT;
ALTER TABLE model.logical_fields
    ADD CONSTRAINT ck_model_logical_field_column CHECK (column_name ~ '^[a-z][a-z0-9_]*$'),
    ADD CONSTRAINT ck_model_logical_field_type CHECK (data_type IN ('string', 'int', 'bigint', 'float', 'decimal', 'date', 'datetime', 'bool', 'json', 'text', 'geometry')),
    ADD CONSTRAINT ck_model_logical_field_length CHECK (length IS NULL OR length > 0),
    ADD CONSTRAINT ck_model_logical_field_role CHECK (field_role IN ('regular', 'measure_additive', 'measure_semi', 'measure_non', 'dimension_fk', 'degenerate_dim')),
    ADD CONSTRAINT ck_model_logical_field_hierarchy CHECK ((hierarchy_id IS NULL AND hierarchy_level IS NULL) OR (hierarchy_id IS NOT NULL AND hierarchy_level IS NOT NULL AND hierarchy_level >= 0)),
    ADD CONSTRAINT fk_model_logical_field_table FOREIGN KEY (table_id) REFERENCES model.logical_tables(id) ON DELETE CASCADE;
ALTER TABLE model.table_relations
    ADD CONSTRAINT ck_model_table_relation_distinct CHECK (source_table <> target_table),
    ADD CONSTRAINT ck_model_table_relation_type CHECK (relation_type IN ('fk', 'join')),
    ADD CONSTRAINT fk_model_table_relation_source_table FOREIGN KEY (source_table, tenant_id) REFERENCES model.logical_tables(id, tenant_id) ON DELETE CASCADE,
    ADD CONSTRAINT fk_model_table_relation_target_table FOREIGN KEY (target_table, tenant_id) REFERENCES model.logical_tables(id, tenant_id) ON DELETE CASCADE,
    ADD CONSTRAINT fk_model_table_relation_source_field FOREIGN KEY (source_field, source_table) REFERENCES model.logical_fields(id, table_id) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_model_table_relation_target_field FOREIGN KEY (target_field, target_table) REFERENCES model.logical_fields(id, table_id) ON DELETE RESTRICT;
ALTER TABLE model.fact_metric_mappings
    ADD CONSTRAINT fk_model_fact_metric_table FOREIGN KEY (fact_table_id, tenant_id) REFERENCES model.logical_tables(id, tenant_id) ON DELETE CASCADE,
    ADD CONSTRAINT fk_model_fact_metric_field FOREIGN KEY (field_id, fact_table_id) REFERENCES model.logical_fields(id, table_id) ON DELETE SET NULL (field_id);

CREATE INDEX idx_model_dw_layers_tenant_sort ON model.dw_layers(tenant_id, sort_order, id);
CREATE INDEX idx_model_entities_tenant_created ON model.entities(tenant_id, created_at DESC, id DESC);
CREATE INDEX idx_model_entities_tenant_domain ON model.entities(tenant_id, domain_id);
CREATE INDEX idx_model_entity_relations_source ON model.entity_relations(tenant_id, source_entity);
CREATE INDEX idx_model_entity_relations_target ON model.entity_relations(tenant_id, target_entity);
CREATE INDEX idx_model_logical_tables_tenant_created ON model.logical_tables(tenant_id, created_at DESC, id DESC);
CREATE INDEX idx_model_logical_tables_tenant_domain ON model.logical_tables(tenant_id, domain_id);
CREATE INDEX idx_model_fact_metrics_table ON model.fact_metric_mappings(tenant_id, fact_table_id);
