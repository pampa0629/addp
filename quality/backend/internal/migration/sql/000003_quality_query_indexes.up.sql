DROP INDEX IF EXISTS quality.idx_ra_tenant_engine;

CREATE INDEX idx_quality_rule_applications_tenant_engine_id
    ON quality.rule_applications (tenant_id, engine_id, id DESC);

CREATE INDEX idx_quality_rule_applications_enabled_scope
    ON quality.rule_applications (tenant_id, engine_id, schema_name, table_name, id ASC)
    WHERE enabled = TRUE;

CREATE INDEX idx_quality_issues_tenant_updated
    ON quality.issues (tenant_id, updated_at DESC, id DESC);

CREATE INDEX idx_quality_issues_tenant_engine_updated
    ON quality.issues (tenant_id, engine_id, updated_at DESC, id DESC);

CREATE INDEX idx_quality_issues_tenant_status_engine_updated
    ON quality.issues (tenant_id, status, engine_id, updated_at DESC, id DESC);
