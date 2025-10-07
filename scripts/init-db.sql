-- 全域数据平台数据库初始化脚本
-- 为各个模块创建独立的 schema

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

CREATE TABLE IF NOT EXISTS metadata.datasets (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL, -- 'table', 'file', 'api'
    source_id INTEGER,
    path TEXT,
    description TEXT,
    schema JSONB,
    statistics JSONB,
    tags TEXT[],
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_datasets_name ON metadata.datasets(name);
CREATE INDEX IF NOT EXISTS idx_datasets_type ON metadata.datasets(type);
CREATE INDEX IF NOT EXISTS idx_datasets_tags ON metadata.datasets USING GIN(tags);

CREATE TABLE IF NOT EXISTS metadata.fields (
    id SERIAL PRIMARY KEY,
    dataset_id INTEGER REFERENCES metadata.datasets(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL,
    nullable BOOLEAN DEFAULT true,
    description TEXT,
    statistics JSONB,
    position INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_fields_dataset ON metadata.fields(dataset_id);

CREATE TABLE IF NOT EXISTS metadata.lineage (
    id SERIAL PRIMARY KEY,
    source_id INTEGER REFERENCES metadata.datasets(id) ON DELETE CASCADE,
    target_id INTEGER REFERENCES metadata.datasets(id) ON DELETE CASCADE,
    type VARCHAR(50) NOT NULL, -- 'transform', 'copy', 'aggregate', 'join'
    transform JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_lineage_source ON metadata.lineage(source_id);
CREATE INDEX IF NOT EXISTS idx_lineage_target ON metadata.lineage(target_id);

-- ==================== Transfer 模块 ====================
CREATE SCHEMA IF NOT EXISTS transfer;

CREATE TABLE IF NOT EXISTS transfer.tasks (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL, -- 'import', 'export', 'sync'
    source_id INTEGER,
    target_id INTEGER,
    config JSONB NOT NULL,
    schedule VARCHAR(100), -- Cron expression
    status VARCHAR(20) DEFAULT 'pending', -- 'pending', 'running', 'success', 'failed', 'paused'
    progress NUMERIC(5,2) DEFAULT 0,
    created_by INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON transfer.tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_type ON transfer.tasks(type);

CREATE TABLE IF NOT EXISTS transfer.task_executions (
    id SERIAL PRIMARY KEY,
    task_id INTEGER REFERENCES transfer.tasks(id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL,
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP,
    records_read BIGINT DEFAULT 0,
    records_written BIGINT DEFAULT 0,
    bytes_read BIGINT DEFAULT 0,
    bytes_written BIGINT DEFAULT 0,
    error_msg TEXT,
    logs TEXT
);

CREATE INDEX IF NOT EXISTS idx_executions_task ON transfer.task_executions(task_id);
CREATE INDEX IF NOT EXISTS idx_executions_status ON transfer.task_executions(status);
CREATE INDEX IF NOT EXISTS idx_executions_start_time ON transfer.task_executions(start_time);

CREATE TABLE IF NOT EXISTS transfer.data_mappings (
    id SERIAL PRIMARY KEY,
    task_id INTEGER REFERENCES transfer.tasks(id) ON DELETE CASCADE,
    source_field VARCHAR(255) NOT NULL,
    target_field VARCHAR(255) NOT NULL,
    transform VARCHAR(500),
    default_value TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- ==================== 创建更新时间戳触发器 ====================
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Manager 模块触发器
CREATE TRIGGER update_data_sources_updated_at BEFORE UPDATE ON manager.data_sources
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_directories_updated_at BEFORE UPDATE ON manager.directories
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Meta 模块触发器
CREATE TRIGGER update_datasets_updated_at BEFORE UPDATE ON metadata.datasets
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
