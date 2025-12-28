-- ========================================
-- 迁移说明: 将 "资源管理" 重构为 "引擎管理"
-- 作者: Claude Code
-- 日期: 2025-12-27
-- ========================================

-- 1. 重命名表: resources → engines
ALTER TABLE system.resources RENAME TO engines;

-- 2. 重命名字段: resource_type → engine_type
ALTER TABLE system.engines RENAME COLUMN resource_type TO engine_type;

-- 3. 新增 engine_category 字段（标准引擎 | 扩展引擎）
ALTER TABLE system.engines
ADD COLUMN engine_category VARCHAR(20) NOT NULL DEFAULT 'standard';

-- 4. 根据现有数据推断引擎分类
-- 扩展引擎: 有 unique_identifier 或 engine_type 以 "api." 开头或等于 "compute_engine"
UPDATE system.engines
SET engine_category = 'extension'
WHERE engine_type IN ('compute_engine', 'api.meta', 'api.transfer', 'api.manager', 'api.geopandas', 'api.jupyter', 'api.spark_sedona')
   OR unique_identifier IS NOT NULL;

-- 标准引擎: 其他所有（PostgreSQL、MySQL、Doris、ClickHouse、MongoDB、Spark SQL、MinIO、S3）
-- (默认值已设为 'standard'，无需更新)

-- 5. 添加约束: 只允许 'standard' 或 'extension'
ALTER TABLE system.engines
ADD CONSTRAINT chk_engine_category
CHECK (engine_category IN ('standard', 'extension'));

-- 6. 创建新索引
CREATE INDEX idx_engines_engine_category ON system.engines(engine_category);
CREATE INDEX idx_engines_engine_type ON system.engines(engine_type);
CREATE INDEX idx_engines_name ON system.engines(name);
CREATE INDEX idx_engines_tenant_id ON system.engines(tenant_id);
CREATE INDEX idx_engines_is_builtin ON system.engines(is_builtin);
CREATE INDEX idx_engines_unique_identifier ON system.engines(unique_identifier);
CREATE INDEX idx_engines_connection_status ON system.engines(connection_status);

-- 7. 删除旧索引（如果存在）
DROP INDEX IF EXISTS idx_name;
DROP INDEX IF EXISTS idx_resource_type;
DROP INDEX IF EXISTS idx_tenant_id;
DROP INDEX IF EXISTS idx_is_builtin;
DROP INDEX IF EXISTS idx_unique_identifier;
DROP INDEX IF EXISTS idx_connection_status;

-- 迁移完成
