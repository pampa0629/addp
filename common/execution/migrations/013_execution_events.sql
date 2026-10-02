CREATE TABLE common.execution_events (
    id BIGSERIAL PRIMARY KEY,
    execution_id VARCHAR(255) NOT NULL REFERENCES common.task_executions(execution_id) ON DELETE CASCADE,
    tenant_id INTEGER NOT NULL CHECK (tenant_id > 0),
    module VARCHAR(50) NOT NULL,
    task_type VARCHAR(50) NOT NULL,
    attempt INTEGER NOT NULL CHECK (attempt > 0),
    occurred_at TIMESTAMPTZ NOT NULL,
    kind VARCHAR(20) NOT NULL CHECK (kind IN ('started','progress','completed','failed','cancelled','timeout','truncated')),
    step_id VARCHAR(128) NOT NULL DEFAULT '',
    counters JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX idx_execution_events_cursor ON common.execution_events (tenant_id, execution_id, id);
CREATE INDEX idx_execution_events_attempt_day ON common.execution_events (execution_id, attempt, occurred_at);
CREATE INDEX idx_execution_events_expiration ON common.execution_events (occurred_at, id);
