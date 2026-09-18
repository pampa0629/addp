DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM common.task_executions WHERE module='quality' AND status IN ('pending','running')) THEN
    RAISE EXCEPTION 'drain active Quality executions before enabling record acceptance';
  END IF;
END $$;

ALTER TABLE quality.issues DROP CONSTRAINT quality_issues_status_check;
ALTER TABLE quality.issues ADD CONSTRAINT quality_issues_status_check CHECK (status IN ('open','resolved','ignored','accepted'));
ALTER TABLE quality.issues
  ADD COLUMN version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
  ADD COLUMN evidence JSONB,
  ADD COLUMN accepted_keys JSONB,
  ADD COLUMN evidence_reason TEXT NOT NULL DEFAULT 'not_observed',
  ADD COLUMN accepted_count BIGINT NOT NULL DEFAULT 0 CHECK (accepted_count >= 0),
  ADD COLUMN pending_count BIGINT NOT NULL DEFAULT 0 CHECK (pending_count >= 0);

CREATE TABLE quality.issue_actions (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  issue_id BIGINT NOT NULL,
  plan_id BIGINT NOT NULL,
  execution_id TEXT NOT NULL,
  action TEXT NOT NULL,
  actor_id BIGINT,
  note TEXT NOT NULL,
  accepted_count BIGINT NOT NULL DEFAULT 0,
  evidence JSONB,
  created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_quality_issue_actions_history ON quality.issue_actions (tenant_id, issue_id, id);
INSERT INTO quality.issue_actions (tenant_id, issue_id, plan_id, execution_id, action, actor_id, note, created_at)
SELECT tenant_id, id, plan_id, last_execution_id, status, resolved_by, COALESCE(resolution_note, ''), COALESCE(resolved_at, updated_at, created_at)
FROM quality.issues WHERE resolved_at IS NOT NULL OR COALESCE(resolution_note, '') <> '';
UPDATE quality.issues SET pending_count = failed_count WHERE status = 'open';
