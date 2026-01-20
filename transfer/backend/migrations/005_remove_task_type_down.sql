-- 回滚：重新添加 tasks 表的 type 字段
-- 注意：原有数据无法恢复

ALTER TABLE transfer.tasks ADD COLUMN IF NOT EXISTS type VARCHAR(50);
