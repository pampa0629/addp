DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM common.task_executions WHERE module='quality' AND task_type IN ('check','data_validation') AND status IN ('pending','running')) THEN
  RAISE EXCEPTION 'drain active Quality executions before migrating quality plans';
 END IF;
END $$;
CREATE TABLE quality.plans (
 id BIGSERIAL PRIMARY KEY,
 tenant_id BIGINT NOT NULL,
 code VARCHAR(100) NOT NULL CHECK (code ~ '^[a-z][a-z0-9_]*$'),
 name VARCHAR(200) NOT NULL,
 description TEXT NOT NULL DEFAULT '',
 version BIGINT NOT NULL DEFAULT 1 CHECK (version>0),
 table_bindings JSONB NOT NULL CHECK (jsonb_typeof(table_bindings)='array'),
 rules JSONB NOT NULL CHECK (rules->>'schema_version'='addp.quality.plan-rules/v1' AND jsonb_typeof(rules->'rules')='array'),
 created_by BIGINT NOT NULL,
 updated_by BIGINT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 last_run_at TIMESTAMPTZ,
 last_execution_id VARCHAR(64) NOT NULL DEFAULT '',
 last_execution_status VARCHAR(20) NOT NULL DEFAULT '',
 UNIQUE (tenant_id,id),
 UNIQUE (tenant_id,code)
);
CREATE INDEX idx_quality_plans_tenant_updated ON quality.plans(tenant_id,updated_at DESC,id DESC);
CREATE INDEX idx_quality_plans_bindings ON quality.plans USING gin(table_bindings);
ALTER TABLE quality.issues DROP CONSTRAINT fk_quality_issue_rule_application;
DROP INDEX quality.uq_quality_issue_rule;
ALTER TABLE quality.issues RENAME COLUMN rule_application_id TO plan_id;
ALTER TABLE quality.issues DROP CONSTRAINT ck_quality_issue_counts;
ALTER TABLE quality.issues ADD CONSTRAINT ck_quality_issue_counts CHECK (failed_count>=0 AND total_count>=0 AND pass_rate BETWEEN 0 AND 100);
-- The runner converts definitions and issue identities using the canonical
-- ResourceLocator codec, then removes retired tables in this transaction.
