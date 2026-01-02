-- 迁移：为 audit_logs 表添加 module_name 字段
-- 日期：2026-01-02
-- 说明：支持跨模块审计日志，记录日志来源模块

-- 添加 module_name 列
ALTER TABLE system.audit_logs ADD COLUMN IF NOT EXISTS module_name VARCHAR(50);

-- 创建索引以优化按模块查询
CREATE INDEX IF NOT EXISTS idx_audit_logs_module ON system.audit_logs(module_name);

-- 更新现有日志的 module_name 为 'system'（默认值）
UPDATE system.audit_logs SET module_name = 'system' WHERE module_name IS NULL OR module_name = '';

-- 添加列注释
COMMENT ON COLUMN system.audit_logs.module_name IS '来源模块（如：system, manager, meta, transfer）';
