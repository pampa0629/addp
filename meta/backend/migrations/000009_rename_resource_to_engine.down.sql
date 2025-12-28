-- ========================================
-- Meta 模块: 回滚 Resource → Engine 重命名
-- 作者: Claude Code
-- 日期: 2025-12-29
-- ========================================

-- 1. metadata.meta_node 表
ALTER TABLE metadata.meta_node RENAME COLUMN engine_id TO res_id;
DROP INDEX IF EXISTS idx_meta_node_engine_tenant;
CREATE INDEX idx_meta_node_res_tenant ON metadata.meta_node(tenant_id, res_id);

-- 2. metadata.meta_item 表
ALTER TABLE metadata.meta_item RENAME COLUMN engine_id TO res_id;
DROP INDEX IF EXISTS idx_meta_item_engine_tenant;
CREATE INDEX idx_meta_item_res_tenant ON metadata.meta_item(tenant_id, res_id);

-- 3. metadata.scan_logs 表
ALTER TABLE metadata.scan_logs RENAME COLUMN engine_id TO resource_id;
DROP INDEX IF EXISTS idx_scan_logs_engine_id;
CREATE INDEX idx_scan_logs_resource_id ON metadata.scan_logs(resource_id);

-- 4. metadata.scan_tasks 表
ALTER TABLE metadata.scan_tasks RENAME COLUMN engine_id TO resource_id;
DROP INDEX IF EXISTS idx_scan_tasks_engine_id;
CREATE INDEX idx_scan_tasks_resource_id ON metadata.scan_tasks(resource_id);

-- 5. metadata.scan_task_runs 表
ALTER TABLE metadata.scan_task_runs RENAME COLUMN engine_id TO resource_id;
DROP INDEX IF EXISTS idx_scan_task_runs_engine_id;
CREATE INDEX idx_scan_task_runs_resource_id ON metadata.scan_task_runs(resource_id);

-- 回滚完成
