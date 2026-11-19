CREATE TABLE IF NOT EXISTS common.task_executions (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id INT NOT NULL,
    execution_id VARCHAR(255) NOT NULL,
    module VARCHAR(50) NOT NULL,
    task_type VARCHAR(100) NOT NULL,
    source VARCHAR(50) NOT NULL DEFAULT '',
    source_task_id VARCHAR(255),
    source_task_name VARCHAR(255),
    parent_execution_id VARCHAR(36),
    status VARCHAR(50) NOT NULL,
    progress INT DEFAULT 0,
    current_step VARCHAR(255),
    execution_boundary VARCHAR(20) NOT NULL DEFAULT 'bounded',
    retry_of_execution_id VARCHAR(255),
    lease_owner VARCHAR(100),
    lease_token CHAR(36),
    lease_expires_at DATETIME(6),
    attempt INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 3,
    trigger_type VARCHAR(50) NOT NULL,
    triggered_by BIGINT,
    actor_principal_id BIGINT,
    actor_tenant_membership_id BIGINT,
    issued_authorization_version BIGINT,
    execution_authorization_id BIGINT,
    authorization_expires_at DATETIME(6),
    execution_config JSON,
    error_details JSON,
    metadata JSON,
    execution_time_ms BIGINT,
    rows_affected BIGINT,
    records_read BIGINT,
    records_written BIGINT,
    bytes_read BIGINT,
    bytes_written BIGINT,
    started_at DATETIME(6),
    completed_at DATETIME(6),
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    cleanup_parent_id VARCHAR(36) GENERATED ALWAYS AS (
        CASE WHEN task_type = 'cleanup_executor' THEN parent_execution_id END
    ) STORED,
    UNIQUE KEY uq_task_execution_id (execution_id),
    UNIQUE KEY uq_task_execution_authorization (execution_authorization_id),
    UNIQUE KEY uq_cleanup_parent_owner (cleanup_parent_id, module),
    KEY idx_task_executions_tenant_status (tenant_id, status),
    KEY idx_task_executions_module_type (module, task_type),
    KEY idx_task_executions_source (source),
    KEY idx_task_executions_parent (parent_execution_id),
    KEY idx_task_executions_created_at (created_at DESC),
    KEY idx_task_executions_retry_of (retry_of_execution_id),
    KEY idx_task_executions_pending (module, execution_boundary, status, created_at, id),
    KEY idx_task_executions_running_lease (module, execution_boundary, status, lease_expires_at, id),
    CHECK (status IN ('pending', 'running', 'success', 'failed', 'timeout', 'cancelled')),
    CHECK (trigger_type IN ('manual', 'scheduled', 'event')),
    CHECK (
        ((actor_principal_id IS NOT NULL) + (actor_tenant_membership_id IS NOT NULL) +
            (issued_authorization_version IS NOT NULL)) IN (0, 3)
        AND (execution_authorization_id IS NULL) = (authorization_expires_at IS NULL)
        AND (execution_authorization_id IS NULL OR actor_principal_id IS NOT NULL)
        AND (actor_principal_id IS NULL OR actor_principal_id > 0)
        AND (actor_tenant_membership_id IS NULL OR actor_tenant_membership_id > 0)
        AND (issued_authorization_version IS NULL OR issued_authorization_version > 0)
        AND (execution_authorization_id IS NULL OR execution_authorization_id > 0)
        AND (authorization_expires_at IS NULL OR authorization_expires_at > created_at)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
