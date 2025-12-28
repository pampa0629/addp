-- ========================================
-- Manager 模块: 回滚 Resource → Engine 重命名
-- 作者: Claude Code
-- 日期: 2025-12-29
-- ========================================

-- manager.quick_view 表
ALTER TABLE manager.quick_view RENAME COLUMN engine_id TO resource_id;
DROP INDEX IF EXISTS idx_quick_view_tenant_engine;
CREATE INDEX idx_quick_view_tenant_resource ON manager.quick_view(tenant_id, resource_id);

-- 回滚完成
