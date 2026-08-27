CREATE TABLE quality.materialization_gate_tasks (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    code VARCHAR(100) NOT NULL,
    name VARCHAR(200) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1,
    materialization_group_id BIGINT NOT NULL,
    materialization_group_version BIGINT NOT NULL,
    table_bindings JSONB NOT NULL,
    assertions JSONB NOT NULL,
    created_by BIGINT NOT NULL,
    updated_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_run_at TIMESTAMPTZ,
    last_execution_id VARCHAR(64) NOT NULL DEFAULT '',
    last_execution_status VARCHAR(20) NOT NULL DEFAULT '',
    CONSTRAINT ck_quality_materialization_gate_code CHECK (code ~ '^[a-z][a-z0-9_]*$'),
    CONSTRAINT ck_quality_materialization_gate_version CHECK (version > 0 AND materialization_group_version > 0),
    CONSTRAINT ck_quality_materialization_gate_bindings CHECK (jsonb_typeof(table_bindings) = 'array' AND jsonb_array_length(table_bindings) > 0),
    CONSTRAINT ck_quality_materialization_gate_assertions CHECK (
        jsonb_typeof(assertions) = 'object'
        AND assertions->>'schema_version' = 'addp.quality.materialization-gate/v1'
        AND jsonb_typeof(assertions->'assertions') = 'array'
        AND jsonb_array_length(assertions->'assertions') > 0
    ),
    CONSTRAINT ck_quality_materialization_gate_last_status CHECK (
        last_execution_status IN ('', 'pending', 'running', 'success', 'failed', 'timeout', 'cancelled')
    )
);

CREATE UNIQUE INDEX uq_quality_materialization_gate_code
    ON quality.materialization_gate_tasks (tenant_id, code);
CREATE INDEX idx_quality_materialization_gate_tenant_updated
    ON quality.materialization_gate_tasks (tenant_id, updated_at DESC, id DESC);
CREATE INDEX idx_quality_materialization_gate_group
    ON quality.materialization_gate_tasks (tenant_id, materialization_group_id);
