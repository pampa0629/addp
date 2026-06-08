-- Orchestrator Step 已收敛为 TaskProvider 任务引用。
-- 旧编排 Step 使用 module/action/method/endpoint，缺少 task_type/task_id，无法可靠迁移为新任务引用。
-- 按 clean break 原则删除旧结构编排定义，避免列表和调度继续消费旧路径。
DELETE FROM orchestrator.orchestrations
WHERE EXISTS (
    SELECT 1
    FROM jsonb_array_elements(steps::jsonb) AS step
    WHERE step ? 'module'
       OR step ? 'action'
       OR step ? 'method'
       OR step ? 'endpoint'
);
