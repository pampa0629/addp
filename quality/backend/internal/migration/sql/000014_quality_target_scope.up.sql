ALTER TABLE quality.issues ADD COLUMN target_key VARCHAR(64);
DROP INDEX quality.uq_quality_issue_rule;
CREATE UNIQUE INDEX uq_quality_issue_rule ON quality.issues (tenant_id, plan_id, target_key, rule_key);
