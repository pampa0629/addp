CREATE TABLE model.standard_reference_guards (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    resource_type VARCHAR(32) NOT NULL,
    resource_id BIGINT NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'open',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_model_standard_reference_guard UNIQUE (tenant_id, resource_type, resource_id),
    CONSTRAINT ck_model_standard_reference_guard_resource_type CHECK (resource_type IN ('domain', 'element', 'dimension_hierarchy', 'metric')),
    CONSTRAINT ck_model_standard_reference_guard_resource_id_positive CHECK (resource_id > 0),
    CONSTRAINT ck_model_standard_reference_guard_state CHECK (state IN ('open', 'frozen', 'deleted'))
);

CREATE INDEX idx_model_standard_reference_guards_tenant_state
    ON model.standard_reference_guards (tenant_id, state);
