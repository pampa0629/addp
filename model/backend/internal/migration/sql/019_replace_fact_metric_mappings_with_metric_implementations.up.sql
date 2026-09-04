-- The legacy mapping lacks a definition revision, grain, source contract and
-- executable expression. It cannot be upgraded without inventing governance
-- facts, so active-development migration deliberately requires recreation.
DROP TABLE IF EXISTS model.fact_metric_mappings CASCADE;

CREATE TABLE model.metric_implementations (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    fact_table_id BIGINT NOT NULL,
    metric_definition_id BIGINT NOT NULL,
    metric_definition_revision_id BIGINT NOT NULL,
    name VARCHAR(200) NOT NULL,
    grain TEXT NOT NULL,
    source_config JSONB NOT NULL,
    dimension_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    filter_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    expression_config JSONB NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    note TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL,
    updated_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_model_metric_implementation_fact_table
        FOREIGN KEY (fact_table_id, tenant_id)
        REFERENCES model.logical_tables(id, tenant_id)
        ON DELETE CASCADE,
    CONSTRAINT uq_model_metric_implementation_identity
        UNIQUE (tenant_id, fact_table_id, metric_definition_revision_id, name),
    CONSTRAINT ck_model_metric_implementation_status
        CHECK (status IN ('active', 'disabled')),
    CONSTRAINT ck_model_metric_implementation_source
        CHECK (jsonb_typeof(source_config) = 'object' AND source_config <> '{}'::jsonb),
    CONSTRAINT ck_model_metric_implementation_dimension
        CHECK (jsonb_typeof(dimension_config) = 'object'),
    CONSTRAINT ck_model_metric_implementation_filter
        CHECK (jsonb_typeof(filter_config) = 'object'),
    CONSTRAINT ck_model_metric_implementation_expression
        CHECK (jsonb_typeof(expression_config) = 'object' AND expression_config <> '{}'::jsonb)
);

CREATE INDEX idx_model_metric_implementations_table
    ON model.metric_implementations(tenant_id, fact_table_id, id);
CREATE INDEX idx_model_metric_implementations_definition
    ON model.metric_implementations(tenant_id, metric_definition_id);
CREATE INDEX idx_model_metric_implementations_revision
    ON model.metric_implementations(tenant_id, metric_definition_revision_id);
