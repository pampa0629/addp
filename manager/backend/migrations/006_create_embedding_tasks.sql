-- ⚠️ 此脚本已废弃（2026-01-09）
-- 表结构现已由 GORM AutoMigrate 管理
-- 见：manager/backend/internal/models/embedding.go (EmbeddingTask 结构体)
-- 见：manager/backend/internal/repository/database.go
--
-- 保留此文件作为参考文档，了解向量化任务表的设计思路
-- 实际表创建由 Manager 模块启动时的 AutoMigrate 自动执行
--
-- ==================== 原始脚本内容（仅供参考） ====================

-- 006_create_embedding_tasks.sql
-- 创建向量化任务执行记录表

CREATE TABLE IF NOT EXISTS manager.embedding_tasks (
    id BIGSERIAL PRIMARY KEY,
    task_id VARCHAR(255) NOT NULL UNIQUE,
    task_type VARCHAR(20) NOT NULL,  -- 任务类型：object（单文件）、directory（目录批量）
    engine_id BIGINT NOT NULL,
    bucket VARCHAR(255) NOT NULL,
    object_key TEXT,  -- 对象路径（单文件任务）
    prefix TEXT,  -- 目录前缀（目录任务）
    recursive BOOLEAN DEFAULT FALSE,  -- 是否递归（目录任务）
    status VARCHAR(20) NOT NULL,  -- 任务状态：pending、running、completed、failed
    started_at TIMESTAMP,  -- 开始时间
    completed_at TIMESTAMP,  -- 完成时间
    duration BIGINT,  -- 执行时长（毫秒）
    total INT DEFAULT 0,  -- 总文件数
    vectorized INT DEFAULT 0,  -- 成功向量化数
    skipped INT DEFAULT 0,  -- 跳过数
    failed INT DEFAULT 0,  -- 失败数
    errors JSONB,  -- 错误详情（JSON数组）
    tenant_id BIGINT,  -- 租户ID
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_embedding_tasks_engine_tenant ON manager.embedding_tasks(engine_id, tenant_id);
CREATE INDEX IF NOT EXISTS idx_embedding_tasks_status ON manager.embedding_tasks(status);
CREATE INDEX IF NOT EXISTS idx_embedding_tasks_task_type ON manager.embedding_tasks(task_type);
CREATE INDEX IF NOT EXISTS idx_embedding_tasks_created_at ON manager.embedding_tasks(created_at DESC);

-- 添加注释
COMMENT ON TABLE manager.embedding_tasks IS '向量化任务执行记录表';
COMMENT ON COLUMN manager.embedding_tasks.task_id IS '任务ID（embedding-{engineID}-{type}-{timestamp}）';
COMMENT ON COLUMN manager.embedding_tasks.task_type IS '任务类型：object（单文件）、directory（目录批量）';
COMMENT ON COLUMN manager.embedding_tasks.status IS '任务状态：pending、running、completed、failed';
COMMENT ON COLUMN manager.embedding_tasks.duration IS '执行时长（毫秒）';
COMMENT ON COLUMN manager.embedding_tasks.errors IS '错误详情（JSON数组）';
