-- Clean break: Develop 当前不声明 owner task schedule 能力。
-- 任务调度只在具备 owner scheduler / next_run_at due claim 闭环的模块中暴露。

ALTER TABLE develop.dev_tasks
    DROP COLUMN IF EXISTS schedule,
    DROP COLUMN IF EXISTS enabled,
    DROP COLUMN IF EXISTS next_run_at;
