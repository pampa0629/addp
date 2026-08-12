CREATE TABLE IF NOT EXISTS quality.rule_applications (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    element_id BIGINT NOT NULL,
    engine_id BIGINT NOT NULL,
    schema_name VARCHAR(200) NOT NULL,
    table_name VARCHAR(200) NOT NULL,
    column_name VARCHAR(200) NOT NULL,
    rule_config JSONB NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by BIGINT NOT NULL,
    updated_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS quality.check_tasks (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    name VARCHAR(200) NOT NULL,
    description VARCHAR(500) NOT NULL DEFAULT '',
    engine_id BIGINT NOT NULL,
    schema_name VARCHAR(200) NOT NULL,
    table_name VARCHAR(200) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by BIGINT NOT NULL,
    updated_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_run_at TIMESTAMPTZ,
    last_execution_id VARCHAR(64) NOT NULL DEFAULT '',
    last_execution_status VARCHAR(20) NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS quality.issues (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    execution_id VARCHAR(255) NOT NULL,
    last_execution_id VARCHAR(255) NOT NULL,
    rule_application_id BIGINT NOT NULL,
    rule_type VARCHAR(100) NOT NULL,
    severity VARCHAR(20) NOT NULL DEFAULT 'error',
    message TEXT NOT NULL DEFAULT '',
    column_name VARCHAR(200) NOT NULL,
    table_name VARCHAR(200) NOT NULL,
    schema_name VARCHAR(200) NOT NULL,
    engine_id BIGINT NOT NULL,
    failed_count BIGINT NOT NULL,
    total_count BIGINT NOT NULL,
    pass_rate DOUBLE PRECISION NOT NULL,
    detail JSONB,
    status VARCHAR(50) NOT NULL DEFAULT 'open',
    resolved_at TIMESTAMPTZ,
    resolved_by BIGINT,
    resolution_note TEXT NOT NULL DEFAULT '',
    last_observed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE quality.check_tasks DROP COLUMN IF EXISTS next_run_at;

UPDATE quality.rule_applications SET enabled = TRUE WHERE enabled IS NULL;
UPDATE quality.rule_applications SET created_at = NOW() WHERE created_at IS NULL;
UPDATE quality.rule_applications SET updated_at = created_at WHERE updated_at IS NULL;
UPDATE quality.check_tasks SET description = '' WHERE description IS NULL;
UPDATE quality.check_tasks SET enabled = TRUE WHERE enabled IS NULL;
UPDATE quality.check_tasks SET created_at = NOW() WHERE created_at IS NULL;
UPDATE quality.check_tasks SET updated_at = created_at WHERE updated_at IS NULL;
UPDATE quality.check_tasks SET last_execution_id = '' WHERE last_execution_id IS NULL;
UPDATE quality.check_tasks SET last_execution_status = '' WHERE last_execution_status IS NULL;
UPDATE quality.issues SET message = '' WHERE message IS NULL;
UPDATE quality.issues SET resolution_note = '' WHERE resolution_note IS NULL;
UPDATE quality.issues SET created_at = NOW() WHERE created_at IS NULL;
UPDATE quality.issues SET updated_at = created_at WHERE updated_at IS NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM quality.rule_applications
        WHERE schema_name = '' OR table_name = '' OR column_name = ''
           OR jsonb_typeof(rule_config) IS DISTINCT FROM 'object'
           OR rule_config->>'schema_version' IS DISTINCT FROM 'addp.quality.rules/v1'
           OR jsonb_typeof(rule_config->'rules') IS DISTINCT FROM 'array'
    ) THEN
        RAISE EXCEPTION 'quality.rule_applications contains data outside addp.quality.rules/v1';
    END IF;
END $$;

ALTER TABLE quality.rule_applications
    ALTER COLUMN enabled SET NOT NULL,
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET NOT NULL;
ALTER TABLE quality.check_tasks
    ALTER COLUMN description SET NOT NULL,
    ALTER COLUMN enabled SET NOT NULL,
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET NOT NULL,
    ALTER COLUMN last_execution_id SET NOT NULL,
    ALTER COLUMN last_execution_status SET NOT NULL;
ALTER TABLE quality.issues
    ALTER COLUMN message SET NOT NULL,
    ALTER COLUMN resolution_note SET NOT NULL,
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET NOT NULL,
    ALTER COLUMN pass_rate TYPE DOUBLE PRECISION USING pass_rate::DOUBLE PRECISION;

ALTER TABLE quality.rule_applications DROP CONSTRAINT IF EXISTS ck_quality_rule_application_scope;
ALTER TABLE quality.rule_applications ADD CONSTRAINT ck_quality_rule_application_scope
    CHECK (schema_name <> '' AND table_name <> '' AND column_name <> '');
ALTER TABLE quality.rule_applications DROP CONSTRAINT IF EXISTS ck_quality_rule_application_contract;
ALTER TABLE quality.rule_applications ADD CONSTRAINT ck_quality_rule_application_contract CHECK (
    jsonb_typeof(rule_config) = 'object'
    AND rule_config->>'schema_version' = 'addp.quality.rules/v1'
    AND jsonb_typeof(rule_config->'rules') = 'array'
);
ALTER TABLE quality.check_tasks DROP CONSTRAINT IF EXISTS ck_quality_check_task_scope;
ALTER TABLE quality.check_tasks ADD CONSTRAINT ck_quality_check_task_scope CHECK (schema_name <> '' AND table_name <> '');
ALTER TABLE quality.check_tasks DROP CONSTRAINT IF EXISTS ck_quality_check_task_last_status;
ALTER TABLE quality.check_tasks ADD CONSTRAINT ck_quality_check_task_last_status CHECK (
    last_execution_status IN ('', 'pending', 'running', 'success', 'failed', 'timeout', 'cancelled')
);
ALTER TABLE quality.issues DROP CONSTRAINT IF EXISTS quality_issues_status_check;
ALTER TABLE quality.issues ADD CONSTRAINT quality_issues_status_check CHECK (status IN ('open', 'resolved', 'ignored'));
ALTER TABLE quality.issues DROP CONSTRAINT IF EXISTS ck_quality_issue_counts;
ALTER TABLE quality.issues ADD CONSTRAINT ck_quality_issue_counts CHECK (
    failed_count >= 0 AND total_count >= 0 AND failed_count <= total_count AND pass_rate >= 0 AND pass_rate <= 100
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_quality_rule_application_scope
    ON quality.rule_applications (tenant_id, element_id, engine_id, schema_name, table_name, column_name);
CREATE UNIQUE INDEX IF NOT EXISTS uq_quality_rule_application_tenant_id
    ON quality.rule_applications (tenant_id, id);
CREATE INDEX IF NOT EXISTS idx_ra_tenant_engine
    ON quality.rule_applications (tenant_id, engine_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_quality_check_task_scope
    ON quality.check_tasks (tenant_id, engine_id, schema_name, table_name);
CREATE INDEX IF NOT EXISTS idx_quality_check_tasks_tenant_updated
    ON quality.check_tasks (tenant_id, updated_at DESC, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uq_quality_issue_rule_application
    ON quality.issues (tenant_id, rule_application_id);
CREATE INDEX IF NOT EXISTS idx_quality_issues_tenant_status_updated
    ON quality.issues (tenant_id, status, updated_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_quality_issues_execution_id
    ON quality.issues (execution_id);
CREATE INDEX IF NOT EXISTS idx_quality_issues_last_execution_id
    ON quality.issues (last_execution_id);

ALTER TABLE quality.issues DROP CONSTRAINT IF EXISTS fk_quality_issue_rule_application;
ALTER TABLE quality.issues ADD CONSTRAINT fk_quality_issue_rule_application
    FOREIGN KEY (tenant_id, rule_application_id)
    REFERENCES quality.rule_applications (tenant_id, id)
    ON DELETE CASCADE;
