-- 创建 Orchestrator Schema
CREATE SCHEMA IF NOT EXISTS orchestrator;

-- 编排定义表
CREATE TABLE IF NOT EXISTS orchestrator.orchestrations (
    id SERIAL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(512),
    steps JSONB NOT NULL,
    enabled BOOLEAN DEFAULT false,
    cron_expr VARCHAR(128),
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_orchestrations_tenant ON orchestrator.orchestrations(tenant_id);
CREATE INDEX IF NOT EXISTS idx_orchestrations_enabled ON orchestrator.orchestrations(enabled);
CREATE INDEX IF NOT EXISTS idx_orchestrations_deleted ON orchestrator.orchestrations(deleted_at);

-- 执行实例表
CREATE TABLE IF NOT EXISTS orchestrator.executions (
    id SERIAL PRIMARY KEY,
    orchestration_id INTEGER REFERENCES orchestrator.orchestrations(id),
    tenant_id INTEGER NOT NULL,
    status VARCHAR(32) NOT NULL,
    current_step VARCHAR(64),
    step_results JSONB,
    error_message TEXT,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_executions_orchestration ON orchestrator.executions(orchestration_id);
CREATE INDEX IF NOT EXISTS idx_executions_tenant ON orchestrator.executions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_executions_status ON orchestrator.executions(status);
CREATE INDEX IF NOT EXISTS idx_executions_created_at ON orchestrator.executions(created_at DESC);

COMMENT ON TABLE orchestrator.orchestrations IS '编排定义表';
COMMENT ON TABLE orchestrator.executions IS '执行实例表';
