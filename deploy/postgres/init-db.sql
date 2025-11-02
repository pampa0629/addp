-- 全域数据平台数据库初始化脚本
-- 为各个模块创建独立的 schema

-- ==================== System 模块 ====================
CREATE SCHEMA IF NOT EXISTS system;

-- 用户表
CREATE TABLE IF NOT EXISTS system.users (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    password VARCHAR(255) NOT NULL,
    email VARCHAR(100) UNIQUE,
    full_name VARCHAR(100),
    role VARCHAR(20) DEFAULT 'user',
    status VARCHAR(20) DEFAULT 'active',
    avatar_url TEXT,
    last_login_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_users_username ON system.users(username);
CREATE INDEX IF NOT EXISTS idx_users_email ON system.users(email);
CREATE INDEX IF NOT EXISTS idx_users_role ON system.users(role);

-- 租户表
CREATE TABLE IF NOT EXISTS system.tenants (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    code VARCHAR(50) UNIQUE NOT NULL,
    description TEXT,
    status VARCHAR(20) DEFAULT 'active',
    admin_user_id BIGINT REFERENCES system.users(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 资源表（数据源配置）
CREATE TABLE IF NOT EXISTS system.resources (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT REFERENCES system.tenants(id),
    name VARCHAR(255) NOT NULL,
    resource_type VARCHAR(50) NOT NULL,
    connection_info JSONB NOT NULL,
    description TEXT,
    status VARCHAR(20) DEFAULT 'active',
    tags TEXT[],
    created_by BIGINT REFERENCES system.users(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_resources_type ON system.resources(resource_type);
CREATE INDEX IF NOT EXISTS idx_resources_tenant ON system.resources(tenant_id);
CREATE INDEX IF NOT EXISTS idx_resources_created_by ON system.resources(created_by);

-- 审计日志表
CREATE TABLE IF NOT EXISTS system.audit_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT REFERENCES system.users(id),
    tenant_id BIGINT,
    action VARCHAR(50) NOT NULL,
    resource_type VARCHAR(50),
    resource_id BIGINT,
    method VARCHAR(10),
    path TEXT,
    ip_address INET,
    user_agent TEXT,
    request_body TEXT,
    response_status INTEGER,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_user ON system.audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON system.audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON system.audit_logs(created_at);

-- 配置表
CREATE TABLE IF NOT EXISTS system.configs (
    id BIGSERIAL PRIMARY KEY,
    key VARCHAR(100) UNIQUE NOT NULL,
    value TEXT NOT NULL,
    description TEXT,
    is_encrypted BOOLEAN DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- ==================== Manager 模块 ====================
CREATE SCHEMA IF NOT EXISTS manager;

CREATE TABLE IF NOT EXISTS manager.data_sources (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL,
    connection_info JSONB NOT NULL,
    status VARCHAR(20) DEFAULT 'active',
    created_by INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS manager.directories (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    parent_id INTEGER REFERENCES manager.directories(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    type VARCHAR(20) NOT NULL, -- 'folder' or 'file'
    size BIGINT DEFAULT 0,
    created_by INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(parent_id, name)
);

CREATE INDEX IF NOT EXISTS idx_directories_parent ON manager.directories(parent_id);
CREATE INDEX IF NOT EXISTS idx_directories_path ON manager.directories(path);

CREATE TABLE IF NOT EXISTS manager.data_source_permissions (
    id SERIAL PRIMARY KEY,
    data_source_id INTEGER REFERENCES manager.data_sources(id) ON DELETE CASCADE,
    user_id INTEGER,
    group_id INTEGER,
    permission VARCHAR(20) NOT NULL, -- 'none', 'read', 'write', 'admin'
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS manager.directory_permissions (
    id SERIAL PRIMARY KEY,
    directory_id INTEGER REFERENCES manager.directories(id) ON DELETE CASCADE,
    user_id INTEGER,
    group_id INTEGER,
    permission VARCHAR(20) NOT NULL,
    inherited BOOLEAN DEFAULT false,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- ==================== Meta 模块 ====================
CREATE SCHEMA IF NOT EXISTS metadata;

DROP TABLE IF EXISTS metadata.fields CASCADE;
DROP TABLE IF EXISTS metadata.lineage CASCADE;
DROP TABLE IF EXISTS metadata.datasets CASCADE;

CREATE TABLE IF NOT EXISTS metadata.meta_node (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    res_id BIGINT NOT NULL,
    parent_node_id BIGINT REFERENCES metadata.meta_node(id) ON DELETE CASCADE,
    node_type VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    depth INT NOT NULL DEFAULT 0,
    path TEXT,
    full_name TEXT,
    status VARCHAR(32) DEFAULT 'active',
    scan_status VARCHAR(32) DEFAULT '未扫描',
    last_scan_at TIMESTAMP WITH TIME ZONE,
    auto_scan_enabled BOOLEAN DEFAULT false,
    auto_scan_cron VARCHAR(128),
    next_scan_at TIMESTAMP WITH TIME ZONE,
    item_count INT DEFAULT 0,
    total_size_bytes BIGINT DEFAULT 0,
    error_message TEXT,
    attributes JSONB DEFAULT '{}'::JSONB,
    sync_version BIGINT DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE,
    UNIQUE (res_id, name, parent_node_id),
    CHECK (depth >= 0)
);

CREATE INDEX IF NOT EXISTS idx_meta_node_res ON metadata.meta_node(res_id);
CREATE INDEX IF NOT EXISTS idx_meta_node_parent ON metadata.meta_node(parent_node_id);
CREATE INDEX IF NOT EXISTS idx_meta_node_type ON metadata.meta_node(node_type);

CREATE TABLE IF NOT EXISTS metadata.meta_item (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    res_id BIGINT NOT NULL,
    node_id BIGINT NOT NULL REFERENCES metadata.meta_node(id) ON DELETE CASCADE,
    item_type VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    full_name TEXT,
    status VARCHAR(32) DEFAULT 'active',
    meta_schema_version INTEGER DEFAULT 1,
    row_count BIGINT,
    size_bytes BIGINT,
    object_size_bytes BIGINT,
    last_modified_at TIMESTAMP WITH TIME ZONE,
    attributes JSONB DEFAULT '{}'::JSONB,
    sync_version BIGINT DEFAULT 0,
    source VARCHAR(64),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE,
    UNIQUE (node_id, name)
);

CREATE INDEX IF NOT EXISTS idx_meta_item_node ON metadata.meta_item(node_id);
CREATE INDEX IF NOT EXISTS idx_meta_item_type ON metadata.meta_item(item_type);

CREATE TABLE IF NOT EXISTS metadata.meta_json_schema (
    id BIGSERIAL PRIMARY KEY,
    target VARCHAR(32) NOT NULL,
    version INTEGER NOT NULL,
    definition JSONB NOT NULL,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (target, version)
);

CREATE TABLE IF NOT EXISTS metadata.meta_change_log (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT,
    res_id BIGINT,
    node_id BIGINT,
    item_id BIGINT,
    change_type VARCHAR(64) NOT NULL,
    change_source VARCHAR(64),
    payload JSONB,
    sync_version BIGINT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (node_id) REFERENCES metadata.meta_node(id) ON DELETE SET NULL,
    FOREIGN KEY (item_id) REFERENCES metadata.meta_item(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS metadata.meta_node_type_dict (
    type_code VARCHAR(64) PRIMARY KEY,
    category VARCHAR(64),
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS metadata.meta_node_child_rule (
    parent_type VARCHAR(64) NOT NULL,
    child_type VARCHAR(64) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (parent_type, child_type),
    FOREIGN KEY (parent_type) REFERENCES metadata.meta_node_type_dict(type_code) ON DELETE CASCADE,
    FOREIGN KEY (child_type) REFERENCES metadata.meta_node_type_dict(type_code) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS metadata.scan_logs (
    id BIGSERIAL PRIMARY KEY,
    resource_id BIGINT NOT NULL,
    schema_id BIGINT,
    tenant_id BIGINT NOT NULL,
    scan_type VARCHAR(50) NOT NULL,
    scan_depth VARCHAR(20),
    target_schemas TEXT,
    status VARCHAR(20) NOT NULL,
    error_message TEXT,
    schemas_scanned INT DEFAULT 0,
    tables_scanned INT DEFAULT 0,
    fields_scanned INT DEFAULT 0,
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    duration_ms BIGINT DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_scan_logs_resource ON metadata.scan_logs(resource_id);
CREATE INDEX IF NOT EXISTS idx_scan_logs_tenant ON metadata.scan_logs(tenant_id);
CREATE INDEX IF NOT EXISTS idx_scan_logs_status ON metadata.scan_logs(status);

-- ==================== Transfer 模块 ====================
CREATE SCHEMA IF NOT EXISTS transfer;

-- 任务表
CREATE TABLE IF NOT EXISTS transfer.tasks (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    type VARCHAR(50) NOT NULL, -- 'import', 'export', 'sync'
    mode VARCHAR(20) DEFAULT 'batch', -- 'batch', 'stream', 'micro-batch'
    source_id INTEGER, -- 关联 system.resources
    target_id INTEGER, -- 关联 system.resources
    config JSONB NOT NULL,
    schedule VARCHAR(100), -- Cron 表达式
    batch_size INTEGER DEFAULT 1000,
    max_parallelism INTEGER DEFAULT 1,
    retry_policy JSONB,
    status VARCHAR(20) DEFAULT 'pending', -- 'pending', 'running', 'success', 'failed', 'paused'
    progress NUMERIC(5,2) DEFAULT 0,
    last_execution_id INTEGER,
    created_by INTEGER,
    tenant_id INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON transfer.tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_type ON transfer.tasks(type);
CREATE INDEX IF NOT EXISTS idx_tasks_tenant ON transfer.tasks(tenant_id);
CREATE INDEX IF NOT EXISTS idx_tasks_source ON transfer.tasks(source_id);
CREATE INDEX IF NOT EXISTS idx_tasks_target ON transfer.tasks(target_id);
CREATE INDEX IF NOT EXISTS idx_tasks_last_execution ON transfer.tasks(last_execution_id);

-- 任务执行记录表
CREATE TABLE IF NOT EXISTS transfer.task_executions (
    id SERIAL PRIMARY KEY,
    task_id INTEGER NOT NULL REFERENCES transfer.tasks(id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL, -- 'pending', 'running', 'success', 'failed', 'cancelled'
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP,
    records_read BIGINT DEFAULT 0,
    records_written BIGINT DEFAULT 0,
    bytes_read BIGINT DEFAULT 0,
    bytes_written BIGINT DEFAULT 0,
    error_msg TEXT,
    logs TEXT,
    checkpoint_offset BIGINT DEFAULT 0,
    checkpoint_state JSONB,
    trigger_type VARCHAR(50), -- 'manual', 'schedule', 'api'
    trigger_by INTEGER
);

CREATE INDEX IF NOT EXISTS idx_executions_task ON transfer.task_executions(task_id);
CREATE INDEX IF NOT EXISTS idx_executions_status ON transfer.task_executions(status);
CREATE INDEX IF NOT EXISTS idx_executions_start_time ON transfer.task_executions(start_time);

-- 字段映射表
CREATE TABLE IF NOT EXISTS transfer.data_mappings (
    id SERIAL PRIMARY KEY,
    task_id INTEGER NOT NULL REFERENCES transfer.tasks(id) ON DELETE CASCADE,
    source_field VARCHAR(255) NOT NULL,
    target_field VARCHAR(255) NOT NULL,
    transform VARCHAR(500),
    default_value TEXT,
    field_type VARCHAR(50),
    format VARCHAR(100),
    nullable BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mappings_task ON transfer.data_mappings(task_id);

-- Checkpoint 表（用于断点续传）
CREATE TABLE IF NOT EXISTS transfer.checkpoints (
    id SERIAL PRIMARY KEY,
    task_id INTEGER NOT NULL,
    execution_id INTEGER NOT NULL,
    offset BIGINT NOT NULL,
    partition_id VARCHAR(255),
    state JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_checkpoint_task_exec ON transfer.checkpoints(task_id, execution_id);
CREATE INDEX IF NOT EXISTS idx_checkpoint_partition ON transfer.checkpoints(partition_id);

-- ==================== 创建更新时间戳触发器 ====================
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- System 模块触发器
CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON system.users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_tenants_updated_at BEFORE UPDATE ON system.tenants
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_resources_updated_at BEFORE UPDATE ON system.resources
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_configs_updated_at BEFORE UPDATE ON system.configs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Manager 模块触发器
CREATE TRIGGER update_data_sources_updated_at BEFORE UPDATE ON manager.data_sources
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_directories_updated_at BEFORE UPDATE ON manager.directories
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Meta 模块触发器
CREATE TRIGGER update_meta_node_updated_at BEFORE UPDATE ON metadata.meta_node
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_meta_item_updated_at BEFORE UPDATE ON metadata.meta_item
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Transfer 模块触发器
CREATE TRIGGER update_tasks_updated_at BEFORE UPDATE ON transfer.tasks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ==================== 创建视图 ====================

-- 数据源统计视图
CREATE OR REPLACE VIEW manager.data_source_stats AS
SELECT
    ds.id,
    ds.name,
    ds.type,
    ds.status,
    COUNT(DISTINCT d.id) as file_count,
    COALESCE(SUM(d.size), 0) as total_size
FROM manager.data_sources ds
LEFT JOIN manager.directories d ON d.created_by = ds.id
WHERE d.type = 'file' OR d.type IS NULL
GROUP BY ds.id, ds.name, ds.type, ds.status;

-- 任务执行统计视图
CREATE OR REPLACE VIEW transfer.task_execution_stats AS
SELECT
    t.id as task_id,
    t.name as task_name,
    COUNT(e.id) as total_executions,
    COUNT(CASE WHEN e.status = 'success' THEN 1 END) as success_count,
    COUNT(CASE WHEN e.status = 'failed' THEN 1 END) as failed_count,
    MAX(e.end_time) as last_execution_time,
    AVG(EXTRACT(EPOCH FROM (e.end_time - e.start_time))) as avg_duration_seconds
FROM transfer.tasks t
LEFT JOIN transfer.task_executions e ON e.task_id = t.id
GROUP BY t.id, t.name;

-- ==================== 插入初始数据 ====================

-- 插入默认租户
INSERT INTO system.tenants (id, name, code, description, status)
VALUES (1, 'Default Tenant', 'default', 'Default tenant for ADDP system', 'active')
ON CONFLICT (id) DO NOTHING;

-- 插入超级管理员
-- Username: SuperAdmin
-- Password: 20251001#SuperAdmin
-- Bcrypt hash with cost=10
INSERT INTO system.users (id, username, password, email, full_name, role, status)
VALUES (
    1,
    'SuperAdmin',
    '$2b$10$y9s54eFqUZB1azqoYsND2OOgNATHmHdZUv94q8DZiKtCT1vh.Af5u',
    'admin@addp.local',
    'Super Administrator',
    'admin',
    'active'
)
ON CONFLICT (username) DO NOTHING;

-- 更新租户的管理员
UPDATE system.tenants SET admin_user_id = 1 WHERE id = 1;

-- 插入系统配置
INSERT INTO system.configs (key, value, description, is_encrypted)
VALUES
    ('system.name', 'ADDP - All Domain Data Platform', 'System name', false),
    ('system.version', '0.0.6', 'Current system version', false),
    ('system.initialized', 'true', 'System initialization flag', false),
    ('system.initialized_at', CURRENT_TIMESTAMP::TEXT, 'System initialization timestamp', false)
ON CONFLICT (key) DO NOTHING;

COMMIT;

-- ==================== 初始化完成 ====================
DO $$
BEGIN
    RAISE NOTICE '==================================================';
    RAISE NOTICE 'ADDP Database Initialization Complete';
    RAISE NOTICE '==================================================';
    RAISE NOTICE 'Schemas created: system, manager, metadata, transfer';
    RAISE NOTICE '';
    RAISE NOTICE 'Super Admin Account:';
    RAISE NOTICE '  Username: SuperAdmin';
    RAISE NOTICE '  Password: 20251001#SuperAdmin';
    RAISE NOTICE '';
    RAISE NOTICE 'IMPORTANT: Change the default password after first login!';
    RAISE NOTICE '==================================================';
END $$;

