-- 回滚：删除资源连接状态缓存字段

DROP INDEX IF EXISTS system.idx_resources_connection_status;

ALTER TABLE system.resources
DROP COLUMN IF EXISTS connection_status,
DROP COLUMN IF EXISTS last_check_at,
DROP COLUMN IF EXISTS check_message;
