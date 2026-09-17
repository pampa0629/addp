ALTER TABLE quality.rules ADD COLUMN owner_domain_id BIGINT NULL;
ALTER TABLE quality.plans ADD COLUMN owner_domain_id BIGINT NULL;
ALTER TABLE quality.issues ADD COLUMN owner_domain_id BIGINT NULL;
CREATE INDEX idx_quality_rules_owner_domain ON quality.rules (tenant_id, owner_domain_id);
CREATE INDEX idx_quality_plans_owner_domain ON quality.plans (tenant_id, owner_domain_id);
CREATE INDEX idx_quality_issues_owner_domain ON quality.issues (tenant_id, owner_domain_id);
CREATE TABLE quality.standard_reference_guards (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id BIGINT NOT NULL,
    state TEXT NOT NULL DEFAULT 'open',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_quality_standard_reference_guard UNIQUE (tenant_id, resource_type, resource_id),
    CONSTRAINT ck_quality_standard_reference_guard_resource_type CHECK (resource_type = 'domain'),
    CONSTRAINT ck_quality_standard_reference_guard_state CHECK (state IN ('open','frozen','deleted'))
);
