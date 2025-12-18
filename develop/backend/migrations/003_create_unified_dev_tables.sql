-- 003_create_unified_dev_tables.sql
-- 创建统一的开发任务表结构

-- 创建统一开发项定义表
CREATE TABLE IF NOT EXISTS develop.dev_items (
    id SERIAL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    name VARCHAR(255) NOT NULL,
    display_name VARCHAR(255),
    dev_type VARCHAR(50) NOT NULL,  -- 'sql' | 'workflow' | 'script'(预留)

    -- 内容存储(根据类型解析)
    content JSONB NOT NULL,  -- SQL文本 / WorkflowDef / Script

    -- 执行配置
    resource_id INTEGER,  -- 关联的资源(数据库/引擎)
    schedule VARCHAR(100),  -- Cron 表达式(可选)
    is_scheduled BOOLEAN DEFAULT false,
    timeout INTEGER DEFAULT 300,

    -- 元数据
    description TEXT,
    tags TEXT[],
    created_by INTEGER,
    updated_by INTEGER,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    deleted_at TIMESTAMP,

    -- 状态
    status VARCHAR(50) DEFAULT 'active',  -- 'active' | 'inactive' | 'archived'
    last_execution_id INTEGER,
    last_execution_status VARCHAR(50),
    last_executed_at TIMESTAMP,

    CONSTRAINT dev_items_unique_name UNIQUE(tenant_id, name)
);

-- 创建索引
CREATE INDEX idx_dev_items_tenant_type ON develop.dev_items(tenant_id, dev_type);
CREATE INDEX idx_dev_items_resource ON develop.dev_items(resource_id);
CREATE INDEX idx_dev_items_status ON develop.dev_items(status);
CREATE INDEX idx_dev_items_scheduled ON develop.dev_items(is_scheduled) WHERE is_scheduled = true;

-- 创建统一执行记录表
CREATE TABLE IF NOT EXISTS develop.dev_executions (
    id SERIAL PRIMARY KEY,
    dev_item_id INTEGER REFERENCES develop.dev_items(id) ON DELETE SET NULL,
    tenant_id INTEGER NOT NULL,
    execution_id VARCHAR(255) UNIQUE NOT NULL,  -- UUID 全局唯一标识

    -- 执行信息
    dev_type VARCHAR(50) NOT NULL,
    trigger_type VARCHAR(50),  -- 'manual' | 'schedule' | 'orchestrator' | 'api'
    triggered_by INTEGER,

    -- 状态跟踪
    status VARCHAR(50) NOT NULL,  -- 'pending' | 'running' | 'success' | 'failed' | 'timeout' | 'cancelled'
    progress INTEGER DEFAULT 0,  -- 0-100
    current_step VARCHAR(255),

    -- 执行结果
    result JSONB,
    error_message TEXT,
    execution_time_ms BIGINT,

    -- 资源使用
    resource_id INTEGER,
    rows_affected BIGINT,
    result_size_bytes BIGINT,

    -- 时间戳
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW()
);

-- 创建索引
CREATE INDEX idx_dev_executions_item ON develop.dev_executions(dev_item_id);
CREATE INDEX idx_dev_executions_tenant_status ON develop.dev_executions(tenant_id, status);
CREATE INDEX idx_dev_executions_trigger_type ON develop.dev_executions(trigger_type);
CREATE INDEX idx_dev_executions_created_at ON develop.dev_executions(created_at DESC);
CREATE INDEX idx_dev_executions_execution_id ON develop.dev_executions(execution_id);

-- 添加注释
COMMENT ON TABLE develop.dev_items IS '统一开发项定义表：存储SQL查询、工作流、脚本等开发任务';
COMMENT ON TABLE develop.dev_executions IS '统一执行记录表：记录所有类型任务的执行历史';

COMMENT ON COLUMN develop.dev_items.dev_type IS '开发项类型：sql(SQL查询)、workflow(工作流)、script(脚本-预留)';
COMMENT ON COLUMN develop.dev_items.content IS 'JSONB格式存储任务内容，根据dev_type解析';
COMMENT ON COLUMN develop.dev_items.status IS '任务状态：active(启用)、inactive(禁用)、archived(归档)';

COMMENT ON COLUMN develop.dev_executions.execution_id IS 'UUID格式的全局唯一执行标识';
COMMENT ON COLUMN develop.dev_executions.status IS '执行状态：pending(等待)、running(运行中)、success(成功)、failed(失败)、timeout(超时)、cancelled(已取消)';
COMMENT ON COLUMN develop.dev_executions.progress IS '执行进度百分比：0-100';
