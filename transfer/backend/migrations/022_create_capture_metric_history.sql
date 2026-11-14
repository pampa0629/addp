BEGIN;
CREATE TABLE IF NOT EXISTS transfer.capture_metric_history (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    task_id BIGINT NOT NULL,
    apply_identity UUID NOT NULL,
    generation BIGINT NOT NULL,
    minute TIMESTAMPTZ NOT NULL,
    point JSONB NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_capture_metric_minute
    ON transfer.capture_metric_history (tenant_id, apply_identity, generation, minute);
CREATE INDEX IF NOT EXISTS idx_capture_metric_expiry ON transfer.capture_metric_history (minute);
COMMIT;
