-- ========================================
-- 回滚说明: 将 "引擎管理" 回滚为 "资源管理"
-- 作者: Claude Code
-- 日期: 2025-12-27
-- ========================================

-- 1. 删除新增的约束
ALTER TABLE system.engines DROP CONSTRAINT IF EXISTS chk_engine_category;

-- 2. 删除新增的 engine_category 字段
ALTER TABLE system.engines DROP COLUMN IF EXISTS engine_category;

-- 3. 重命名字段: engine_type → resource_type
ALTER TABLE system.engines RENAME COLUMN engine_type TO resource_type;

-- 4. 重命名表: engines → resources
ALTER TABLE system.engines RENAME TO resources;

-- 5. 删除新索引
DROP INDEX IF EXISTS idx_engines_name;
DROP INDEX IF EXISTS idx_engines_tenant_id;
DROP INDEX IF EXISTS idx_engines_is_builtin;
DROP INDEX IF EXISTS idx_engines_unique_identifier;
DROP INDEX IF EXISTS idx_engines_connection_status;
DROP INDEX IF EXISTS idx_engines_engine_category;
DROP INDEX IF EXISTS idx_engines_engine_type;

-- 6. 恢复旧索引
CREATE INDEX idx_name ON system.resources(name);
CREATE INDEX idx_resource_type ON system.resources(resource_type);
CREATE INDEX idx_tenant_id ON system.resources(tenant_id);
CREATE INDEX idx_is_builtin ON system.resources(is_builtin);
CREATE INDEX idx_unique_identifier ON system.resources(unique_identifier);
CREATE INDEX idx_connection_status ON system.resources(connection_status);

-- 回滚完成
