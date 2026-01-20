-- 回滚：重新添加 field_mappings 表的 transform 字段
-- 注意：原有数据无法恢复

ALTER TABLE transfer.field_mappings ADD COLUMN IF NOT EXISTS transform VARCHAR(500);
