-- 全域数据平台数据库初始化脚本
-- 为各个模块创建独立的 schema
--
-- 空间数据与向量扩展支持:
-- - PostGIS: 空间数据类型和空间函数 (geometry, geography等)
-- - pgvector: 向量嵌入和相似度搜索 (vector类型, 支持多模态AI应用)

-- ==================== PostGIS 扩展 ====================
-- PostGIS 扩展预装于 PostGIS 镜像中
-- ARM64: imresamu/postgis-arm64:15-3.4
-- AMD64: postgis/postgis:15-3.4
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS postgis_topology;

-- ==================== pgvector 扩展 ====================
-- Note: pgvector 扩展由 infra-init-pgvector.sh 脚本安装
-- (需要从源码编译,不在包仓库中)
-- 用于多模态向量检索、相似度搜索、最近邻查询等场景

-- ==================== System 模块 ====================
CREATE SCHEMA IF NOT EXISTS system;

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
    name TEXT,
    parent_id BIGINT REFERENCES manager.directories(id) ON DELETE CASCADE,
    path TEXT,
    type TEXT,
    size BIGINT,
    mime_type TEXT,
    storage_id BIGINT,
    created_by BIGINT,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
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
    fingerprint VARCHAR(64) NOT NULL,
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
CREATE UNIQUE INDEX IF NOT EXISTS idx_meta_item_fingerprint ON metadata.meta_item(fingerprint);

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
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    type VARCHAR(50) NOT NULL, -- 'import', 'export', 'sync'
    mode VARCHAR(20) DEFAULT 'batch', -- 'batch', 'stream', 'micro-batch'
    source_id BIGINT, -- 关联 system.resources
    target_id BIGINT, -- 关联 system.resources
    config JSONB NOT NULL,
    schedule VARCHAR(100), -- Cron 表达式
    batch_size INTEGER DEFAULT 1000,
    max_parallelism INTEGER DEFAULT 1,
    retry_policy JSONB,
    status VARCHAR(20) DEFAULT 'pending', -- 'pending', 'running', 'success', 'failed', 'paused'
    progress NUMERIC(5,2) DEFAULT 0,
    last_execution_id BIGINT,
    created_by BIGINT,
    tenant_id BIGINT NOT NULL,
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
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES transfer.tasks(id) ON DELETE CASCADE,
    status TEXT NOT NULL, -- 'pending', 'running', 'success', 'failed', 'cancelled'
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
    trigger_by BIGINT
);

CREATE INDEX IF NOT EXISTS idx_executions_task ON transfer.task_executions(task_id);
CREATE INDEX IF NOT EXISTS idx_executions_status ON transfer.task_executions(status);
CREATE INDEX IF NOT EXISTS idx_executions_start_time ON transfer.task_executions(start_time);

-- 字段映射表
CREATE TABLE IF NOT EXISTS transfer.data_mappings (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES transfer.tasks(id) ON DELETE CASCADE,
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
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL,
    execution_id BIGINT NOT NULL,
    "offset" BIGINT NOT NULL,
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

-- Manager 模块触发器
DROP TRIGGER IF EXISTS update_data_sources_updated_at ON manager.data_sources;
CREATE TRIGGER update_data_sources_updated_at BEFORE UPDATE ON manager.data_sources
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_directories_updated_at ON manager.directories;
CREATE TRIGGER update_directories_updated_at BEFORE UPDATE ON manager.directories
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Meta 模块触发器
DROP TRIGGER IF EXISTS update_meta_node_updated_at ON metadata.meta_node;
CREATE TRIGGER update_meta_node_updated_at BEFORE UPDATE ON metadata.meta_node
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_meta_item_updated_at ON metadata.meta_item;
CREATE TRIGGER update_meta_item_updated_at BEFORE UPDATE ON metadata.meta_item
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Transfer 模块触发器
DROP TRIGGER IF EXISTS update_tasks_updated_at ON transfer.tasks;
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

-- ==================== 插入示例数据（可选）====================

-- Manager: 示例数据源
-- INSERT INTO manager.data_sources (name, type, connection_info, created_by) VALUES
-- ('Sample MySQL', 'mysql', '{"host": "localhost", "port": 3306, "database": "test"}', 1);

-- Meta: 示例数据集
-- INSERT INTO metadata.datasets (name, type, source_id, description) VALUES
-- ('users_table', 'table', 1, 'User information table');

-- Transfer: 示例任务
-- INSERT INTO transfer.tasks (name, type, config, created_by) VALUES
-- ('Daily User Sync', 'sync', '{"batch_size": 1000}', 1);

COMMIT;
