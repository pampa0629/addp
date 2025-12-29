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
-- pgvector 扩展已通过自定义 Docker 镜像预装 (Dockerfile.postgres)
-- (从源码编译 v0.8.0,支持向量检索和相似度搜索)
-- 用于多模态向量检索、相似度搜索、最近邻查询等场景
CREATE EXTENSION IF NOT EXISTS vector;

-- ==================== System 模块 ====================
CREATE SCHEMA IF NOT EXISTS system;

-- 应用管理表（Application Management）
CREATE TABLE IF NOT EXISTS system.applications (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,

    -- 所属租户
    tenant_id INTEGER NOT NULL,

    -- 权限配置
    allowed_services TEXT[],                    -- 允许访问的服务列表
    rate_limit_per_minute INTEGER DEFAULT 60,   -- 限流配置（每分钟请求数）

    -- 状态
    status VARCHAR(50) DEFAULT 'active',        -- active/suspended/revoked

    -- 审计
    created_by INTEGER,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    deleted_at TIMESTAMP,

    UNIQUE(tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_applications_tenant ON system.applications(tenant_id);
CREATE INDEX IF NOT EXISTS idx_applications_status ON system.applications(status);

-- API Key 管理表
CREATE TABLE IF NOT EXISTS system.api_keys (
    id SERIAL PRIMARY KEY,
    application_id INTEGER NOT NULL REFERENCES system.applications(id) ON DELETE CASCADE,

    -- API Key（只存储 hash）
    key_prefix VARCHAR(20) NOT NULL,            -- "addp_live_" 前缀（明文存储，用于识别）
    key_hash VARCHAR(64) NOT NULL,              -- SHA256 hash（用于验证）

    -- 元数据
    name VARCHAR(255),                          -- Key 名称（便于识别多个 Key）
    last_used_at TIMESTAMP,                     -- 最后使用时间

    -- 生命周期
    expires_at TIMESTAMP,                       -- 过期时间
    status VARCHAR(50) DEFAULT 'active',        -- active/revoked

    -- 审计
    created_by INTEGER,
    created_at TIMESTAMP DEFAULT NOW(),
    revoked_at TIMESTAMP,
    revoked_by INTEGER,

    UNIQUE(key_hash)
);

CREATE INDEX IF NOT EXISTS idx_api_keys_application ON system.api_keys(application_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON system.api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_status ON system.api_keys(status);

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

-- 快显任务表
CREATE TABLE IF NOT EXISTS manager.quick_view (
    id SERIAL PRIMARY KEY,
    tenant_id INT NOT NULL,
    engine_id INT NOT NULL,
    schema_name VARCHAR(255) NOT NULL,
    table_name VARCHAR(255) NOT NULL,

    -- 快显状态
    status VARCHAR(50) NOT NULL DEFAULT 'none', -- 'none', 'generating', 'ready', 'failed'
    error_message TEXT,

    -- 缓存配置
    min_zoom INT, -- 自动计算得到（不设默认值）
    max_zoom INT DEFAULT 18,
    actual_max_zoom INT, -- 实际生成到第几层

    -- 统计信息
    total_tiles INT DEFAULT 0,
    cached_tiles INT DEFAULT 0,
    last_zoom_avg_time_ms FLOAT, -- 最后一层的平均生成时间
    last_zoom_avg_size_kb FLOAT, -- 最后一层的平均瓦片大小

    -- 停止条件配置
    stop_threshold_time_ms FLOAT DEFAULT 300,
    stop_threshold_size_kb FLOAT DEFAULT 100,

    -- 指纹（用于 MinIO 路径）
    fingerprint VARCHAR(64) NOT NULL,

    -- 空间范围（存储快照，避免依赖 meta）
    extent JSONB, -- [minLng, minLat, maxLng, maxLat]

    -- 时间戳
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(tenant_id, engine_id, schema_name, table_name)
);

CREATE INDEX IF NOT EXISTS idx_quick_view_status ON manager.quick_view(status);
CREATE INDEX IF NOT EXISTS idx_quick_view_fingerprint ON manager.quick_view(fingerprint);

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
    engine_id BIGINT NOT NULL,
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
    UNIQUE (engine_id, name, parent_node_id),
    CHECK (depth >= 0)
);

CREATE INDEX IF NOT EXISTS idx_meta_node_engine ON metadata.meta_node(engine_id);
CREATE INDEX IF NOT EXISTS idx_meta_node_parent ON metadata.meta_node(parent_node_id);
CREATE INDEX IF NOT EXISTS idx_meta_node_type ON metadata.meta_node(node_type);

CREATE TABLE IF NOT EXISTS metadata.meta_item (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    engine_id BIGINT NOT NULL,
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
    engine_id BIGINT,
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
    engine_id BIGINT NOT NULL,
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

CREATE INDEX IF NOT EXISTS idx_scan_logs_engine ON metadata.scan_logs(engine_id);
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
    source_id BIGINT, -- 关联 system.engines
    target_id BIGINT, -- 关联 system.engines
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

DROP TRIGGER IF EXISTS update_quick_view_updated_at ON manager.quick_view;
CREATE TRIGGER update_quick_view_updated_at BEFORE UPDATE ON manager.quick_view
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- System 模块触发器
DROP TRIGGER IF EXISTS update_applications_updated_at ON system.applications;
CREATE TRIGGER update_applications_updated_at BEFORE UPDATE ON system.applications
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

-- ==================== Develop 模块 (SQL 开发工作台) ====================
CREATE SCHEMA IF NOT EXISTS develop;

-- SQL 脚本表
CREATE TABLE IF NOT EXISTS develop.scripts (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    sql_content TEXT NOT NULL,
    engine_id INTEGER NOT NULL REFERENCES system.engines(id),
    version INTEGER DEFAULT 1,
    status VARCHAR(20) DEFAULT 'draft',  -- draft, published, archived
    created_by INTEGER NOT NULL REFERENCES system.users(id),
    tenant_id INTEGER NOT NULL REFERENCES system.tenants(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    CONSTRAINT unique_script_name_per_tenant UNIQUE(tenant_id, name, deleted_at)
);

CREATE INDEX IF NOT EXISTS idx_develop_scripts_tenant ON develop.scripts(tenant_id);
CREATE INDEX IF NOT EXISTS idx_develop_scripts_status ON develop.scripts(status);
CREATE INDEX IF NOT EXISTS idx_develop_scripts_engine ON develop.scripts(engine_id);
CREATE INDEX IF NOT EXISTS idx_develop_scripts_created_by ON develop.scripts(created_by);

-- 脚本版本历史表
CREATE TABLE IF NOT EXISTS develop.script_versions (
    id SERIAL PRIMARY KEY,
    script_id INTEGER NOT NULL REFERENCES develop.scripts(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    sql_content TEXT NOT NULL,
    change_description TEXT,
    created_by INTEGER NOT NULL REFERENCES system.users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT unique_script_version UNIQUE(script_id, version)
);

CREATE INDEX IF NOT EXISTS idx_develop_versions_script ON develop.script_versions(script_id);

-- 执行记录表
CREATE TABLE IF NOT EXISTS develop.executions (
    id SERIAL PRIMARY KEY,
    script_id INTEGER REFERENCES develop.scripts(id) ON DELETE SET NULL,
    engine_id INTEGER NOT NULL REFERENCES system.engines(id),
    sql_content TEXT NOT NULL,
    status VARCHAR(20) NOT NULL,  -- running, success, failed, timeout
    rows_affected INTEGER,
    execution_time_ms INTEGER,
    error_message TEXT,
    executed_by INTEGER NOT NULL REFERENCES system.users(id),
    tenant_id INTEGER NOT NULL REFERENCES system.tenants(id),
    started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_develop_executions_script ON develop.executions(script_id);
CREATE INDEX IF NOT EXISTS idx_develop_executions_tenant ON develop.executions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_develop_executions_started ON develop.executions(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_develop_executions_status ON develop.executions(status);

-- 脚本依赖关系表
CREATE TABLE IF NOT EXISTS develop.script_dependencies (
    id SERIAL PRIMARY KEY,
    script_id INTEGER NOT NULL REFERENCES develop.scripts(id) ON DELETE CASCADE,
    depends_on_script_id INTEGER NOT NULL REFERENCES develop.scripts(id) ON DELETE CASCADE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT unique_dependency UNIQUE(script_id, depends_on_script_id),
    CONSTRAINT no_self_dependency CHECK (script_id != depends_on_script_id)
);

CREATE INDEX IF NOT EXISTS idx_develop_deps_script ON develop.script_dependencies(script_id);
CREATE INDEX IF NOT EXISTS idx_develop_deps_depends ON develop.script_dependencies(depends_on_script_id);

-- 空间任务定义表（GIS 工作流任务）
CREATE TABLE IF NOT EXISTS develop.spatial_tasks (
    id SERIAL PRIMARY KEY,
    tenant_id INTEGER NOT NULL REFERENCES system.tenants(id),
    name VARCHAR(128) NOT NULL,
    description TEXT,
    workflow_def JSONB NOT NULL,       -- DAG 定义（算子链）
    input_schema JSONB,                -- 参数定义（参数化）
    output_schema JSONB,               -- 输出定义
    schedule VARCHAR(100),             -- Cron 表达式（可选）
    status VARCHAR(20) DEFAULT 'active',
    last_execution_id UUID,
    last_execution_status VARCHAR(20),
    last_execution_started_at TIMESTAMP,
    last_execution_finished_at TIMESTAMP,
    created_by INTEGER REFERENCES system.users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    CONSTRAINT unique_spatial_task_name_per_tenant UNIQUE(tenant_id, name, deleted_at)
);

CREATE INDEX IF NOT EXISTS idx_spatial_tasks_tenant ON develop.spatial_tasks(tenant_id);
CREATE INDEX IF NOT EXISTS idx_spatial_tasks_status ON develop.spatial_tasks(status);
CREATE INDEX IF NOT EXISTS idx_spatial_tasks_created_by ON develop.spatial_tasks(created_by);

-- 空间任务执行实例表（GIS 任务执行历史）
CREATE TABLE IF NOT EXISTS develop.spatial_executions (
    id SERIAL PRIMARY KEY,
    task_id INTEGER REFERENCES develop.spatial_tasks(id) ON DELETE SET NULL,
    task_name VARCHAR(255),                -- 冗余任务名（方便查询）
    status VARCHAR(20) NOT NULL,           -- pending, running, success, failed, timeout
    inputs JSONB,                          -- 输入参数
    result_table VARCHAR(255),             -- 结果表名（spatial_execution_results_<exec_id>）
    result_count INTEGER,                  -- 结果记录数
    error_message TEXT,
    logs TEXT,                             -- 执行日志
    execution_time_ms INTEGER,             -- 执行时间（毫秒）
    trigger_type VARCHAR(50),              -- manual, schedule, orchestrator, api
    trigger_by INTEGER REFERENCES system.users(id),
    tenant_id INTEGER NOT NULL REFERENCES system.tenants(id),
    started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_spatial_executions_task_id ON develop.spatial_executions(task_id);
CREATE INDEX IF NOT EXISTS idx_spatial_executions_tenant_id ON develop.spatial_executions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_spatial_executions_status ON develop.spatial_executions(status);
CREATE INDEX IF NOT EXISTS idx_spatial_executions_started_at ON develop.spatial_executions(started_at DESC);

-- 空间执行结果表（Develop 即时执行结果）
CREATE TABLE IF NOT EXISTS develop.spatial_execution_results (
    id SERIAL PRIMARY KEY,
    execution_id UUID NOT NULL,
    task_id INTEGER REFERENCES develop.spatial_tasks(id) ON DELETE SET NULL,
    tenant_id INTEGER NOT NULL REFERENCES system.tenants(id),
    geom GEOMETRY(GEOMETRY, 4326),        -- 空间字段
    properties JSONB,                     -- 属性数据
    created_by INTEGER REFERENCES system.users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_spatial_exec_results_tenant ON develop.spatial_execution_results(tenant_id);
CREATE INDEX IF NOT EXISTS idx_spatial_exec_results_execution ON develop.spatial_execution_results(execution_id);
CREATE INDEX IF NOT EXISTS idx_spatial_exec_results_geom ON develop.spatial_execution_results USING GIST(geom);
CREATE INDEX IF NOT EXISTS idx_spatial_exec_results_task ON develop.spatial_execution_results(task_id);

-- Develop 模块触发器
DROP TRIGGER IF EXISTS update_scripts_updated_at ON develop.scripts;
CREATE TRIGGER update_scripts_updated_at BEFORE UPDATE ON develop.scripts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_spatial_tasks_updated_at ON develop.spatial_tasks;
CREATE TRIGGER update_spatial_tasks_updated_at BEFORE UPDATE ON develop.spatial_tasks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Note: Orchestrator schema is initialized separately via scripts/init-orchestrator.sql
-- This is handled by infra-init-db.sh script

-- ==================== Orchestrator 模块 ====================
CREATE SCHEMA IF NOT EXISTS orchestrator;

-- ==================== Gateway 模块 ====================
CREATE SCHEMA IF NOT EXISTS gateway;

-- API 访问日志表
CREATE TABLE IF NOT EXISTS gateway.api_access_logs (
    id BIGSERIAL PRIMARY KEY,

    -- API Key 信息
    application_id INTEGER,                    -- 关联 system.applications
    api_key_prefix VARCHAR(20),                -- Key 前缀（用于识别）

    -- 请求信息
    service_name VARCHAR(255),                 -- 目标服务（system, manager, meta等）
    request_method VARCHAR(10),                -- GET, POST, PUT, DELETE
    request_path TEXT,                         -- 请求路径
    request_params JSONB,                      -- 请求参数（query + body）

    -- 响应信息
    response_status INTEGER,                   -- HTTP 状态码
    response_time_ms INTEGER,                  -- 响应时间（毫秒）

    -- 性能指标
    cache_hit BOOLEAN DEFAULT FALSE,           -- 是否缓存命中
    rate_limited BOOLEAN DEFAULT FALSE,        -- 是否被限流

    -- 时间戳
    accessed_at TIMESTAMP DEFAULT NOW(),

    -- 索引优化
    CONSTRAINT chk_response_status CHECK (response_status >= 100 AND response_status < 600)
);

-- 索引：按应用查询
CREATE INDEX IF NOT EXISTS idx_access_logs_application ON gateway.api_access_logs(application_id, accessed_at DESC);

-- 索引：按时间查询（用于清理和统计）
CREATE INDEX IF NOT EXISTS idx_access_logs_accessed_at ON gateway.api_access_logs(accessed_at DESC);

-- 索引：按服务查询
CREATE INDEX IF NOT EXISTS idx_access_logs_service ON gateway.api_access_logs(service_name, accessed_at DESC);

-- 索引：限流统计
CREATE INDEX IF NOT EXISTS idx_access_logs_rate_limited ON gateway.api_access_logs(rate_limited, accessed_at DESC) WHERE rate_limited = TRUE;

COMMIT;
