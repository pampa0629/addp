CREATE TABLE IF NOT EXISTS common.execution_events (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    execution_id VARCHAR(255) NOT NULL,
    tenant_id INT NOT NULL CHECK (tenant_id > 0),
    module VARCHAR(50) NOT NULL,
    task_type VARCHAR(50) NOT NULL,
    attempt INT NOT NULL CHECK (attempt > 0),
    occurred_at DATETIME(6) NOT NULL,
    kind VARCHAR(20) NOT NULL CHECK (kind IN ('started','progress','completed','failed','cancelled','timeout','truncated')),
    step_id VARCHAR(128) NOT NULL DEFAULT '',
    counters JSON NOT NULL DEFAULT (JSON_OBJECT()),
    FOREIGN KEY (execution_id) REFERENCES common.task_executions(execution_id) ON DELETE CASCADE,
    KEY idx_execution_events_cursor (tenant_id, execution_id, id),
    KEY idx_execution_events_attempt_day (execution_id, attempt, occurred_at),
    KEY idx_execution_events_expiration (occurred_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
