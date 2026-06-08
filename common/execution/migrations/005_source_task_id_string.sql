-- source_task_id 是跨模块任务定义 ID 的字符串软引用。
-- 数值型 owner ID 按十进制字符串保存，不建立跨 schema 外键。

ALTER TABLE common.task_executions
ALTER COLUMN source_task_id TYPE VARCHAR(255)
USING source_task_id::text;
