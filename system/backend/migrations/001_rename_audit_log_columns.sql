-- 迁移：重命名 audit_logs 表字段
-- 日期：2026-01-01
-- 说明：将 engine_type 和 engine_id 重命名为 entity_type 和 entity_id

-- 重命名字段
ALTER TABLE system.audit_logs RENAME COLUMN engine_type TO entity_type;
ALTER TABLE system.audit_logs RENAME COLUMN engine_id TO entity_id;

-- 添加字段注释
COMMENT ON COLUMN system.audit_logs.entity_type IS '资源类型（如：user, engine, tenant, dataset）';
COMMENT ON COLUMN system.audit_logs.entity_id IS '资源ID';
COMMENT ON COLUMN system.audit_logs.details IS '操作详情（JSON格式）';
