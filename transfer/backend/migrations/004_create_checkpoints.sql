-- 创建 checkpoints 表，用于支持断点续传功能
CREATE TABLE IF NOT EXISTS transfer.checkpoints (
    id SERIAL PRIMARY KEY,
    task_id INTEGER NOT NULL,
    execution_id INTEGER NOT NULL,
    "offset" BIGINT NOT NULL,
    partition_id VARCHAR(255),
    state JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 创建复合索引，加速查询
CREATE INDEX IF NOT EXISTS idx_checkpoint_task_exec
ON transfer.checkpoints(task_id, execution_id);

-- 添加注释
COMMENT ON TABLE transfer.checkpoints IS 'Task execution checkpoints for resumable transfers';
COMMENT ON COLUMN transfer.checkpoints.task_id IS 'Reference to transfer.tasks.id';
COMMENT ON COLUMN transfer.checkpoints.execution_id IS 'Reference to transfer.task_executions.id';
COMMENT ON COLUMN transfer.checkpoints."offset" IS 'Current offset in the data source';
COMMENT ON COLUMN transfer.checkpoints.partition_id IS 'Partition identifier for parallel processing';
COMMENT ON COLUMN transfer.checkpoints.state IS 'Additional state information in JSON format';
