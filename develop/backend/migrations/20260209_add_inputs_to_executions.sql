-- 添加 inputs 字段到 dev_executions 表
-- 用于存储执行时的输入参数

ALTER TABLE develop.dev_executions
ADD COLUMN IF NOT EXISTS inputs jsonb;

COMMENT ON COLUMN develop.dev_executions.inputs IS '执行时的输入参数（JSONB格式）';
