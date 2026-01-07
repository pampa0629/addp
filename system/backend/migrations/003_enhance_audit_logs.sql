-- 003: 审计日志表增强
-- 目的：
-- 1. 新增关键字段（HTTP状态码、请求耗时、日志级别、错误信息、请求ID）
-- 2. 优化索引（删除重复索引、创建复合索引）
-- 3. 数据迁移（填充系统用户信息）

BEGIN;

-- ========================================
-- 1. 新增字段
-- ========================================

ALTER TABLE system.audit_logs
ADD COLUMN IF NOT EXISTS http_status INTEGER,              -- HTTP状态码 (200, 404, 500...)
ADD COLUMN IF NOT EXISTS duration_ms INTEGER,              -- 请求耗时 (毫秒)
ADD COLUMN IF NOT EXISTS log_level VARCHAR(10) DEFAULT 'INFO', -- 日志级别 (INFO/WARN/ERROR)
ADD COLUMN IF NOT EXISTS error_message TEXT,               -- 错误信息 (仅失败请求)
ADD COLUMN IF NOT EXISTS request_id VARCHAR(64);           -- 请求追踪 ID (UUID)

-- ========================================
-- 2. 数据迁移 - 填充系统用户信息
-- ========================================

-- 将所有 user_id 为 NULL 的记录标记为系统内部调用
UPDATE system.audit_logs
SET
  user_id = 0,
  username = CASE
    WHEN action LIKE '%/internal/%' THEN 'SYSTEM_INTERNAL'
    WHEN action LIKE '%/api/auth/login%' THEN 'SYSTEM_ANONYMOUS'
    ELSE 'SYSTEM_UNKNOWN'
  END
WHERE user_id IS NULL;

-- 填充默认日志级别（根据现有记录推断）
UPDATE system.audit_logs
SET log_level = 'INFO'
WHERE log_level IS NULL;

-- ========================================
-- 3. 索引优化
-- ========================================

-- 删除重复的模块名索引（保留 idx_audit_logs_module_name）
DROP INDEX IF EXISTS system.idx_audit_logs_module;

-- 创建新的复合索引（提升常见查询性能）

-- 按租户+时间查询（最常见）
CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant_created
ON system.audit_logs(tenant_id, created_at DESC);

-- 按模块+时间查询
CREATE INDEX IF NOT EXISTS idx_audit_logs_module_created
ON system.audit_logs(module_name, created_at DESC);

-- 按用户+时间查询
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_created
ON system.audit_logs(user_id, created_at DESC);

-- 按状态码筛选（用于错误分析）
CREATE INDEX IF NOT EXISTS idx_audit_logs_status
ON system.audit_logs(http_status);

-- 按日志级别筛选
CREATE INDEX IF NOT EXISTS idx_audit_logs_level
ON system.audit_logs(log_level);

-- ========================================
-- 4. 添加注释
-- ========================================

COMMENT ON COLUMN system.audit_logs.http_status IS 'HTTP状态码，用于区分成功(2xx)/客户端错误(4xx)/服务器错误(5xx)';
COMMENT ON COLUMN system.audit_logs.duration_ms IS '请求耗时（毫秒），用于性能监控和慢请求识别';
COMMENT ON COLUMN system.audit_logs.log_level IS '日志级别：INFO（正常操作）、WARN（警告）、ERROR（错误）';
COMMENT ON COLUMN system.audit_logs.error_message IS '错误信息，仅在请求失败时记录';
COMMENT ON COLUMN system.audit_logs.request_id IS '请求追踪ID，用于跨模块请求关联';

COMMIT;

-- ========================================
-- 验证脚本
-- ========================================

-- 检查新字段是否创建成功
SELECT
    column_name,
    data_type,
    is_nullable,
    column_default
FROM information_schema.columns
WHERE table_schema = 'system'
  AND table_name = 'audit_logs'
  AND column_name IN ('http_status', 'duration_ms', 'log_level', 'error_message', 'request_id')
ORDER BY column_name;

-- 检查索引是否创建成功
SELECT
    indexname,
    indexdef
FROM pg_indexes
WHERE schemaname = 'system'
  AND tablename = 'audit_logs'
ORDER BY indexname;

-- 检查系统用户填充情况
SELECT
    COALESCE(username, 'NULL') AS username,
    COUNT(*) AS count
FROM system.audit_logs
WHERE user_id = 0 OR user_id IS NULL
GROUP BY username
ORDER BY count DESC;
