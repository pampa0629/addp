-- ========================================
-- Manager 模块: Resource → Engine 重命名
-- 作者: Claude Code
-- 日期: 2025-12-29
-- ========================================

-- manager.quick_view 表
ALTER TABLE manager.quick_view RENAME COLUMN resource_id TO engine_id;
DROP INDEX IF EXISTS idx_quick_view_tenant_resource;
CREATE INDEX idx_quick_view_tenant_engine ON manager.quick_view(tenant_id, engine_id);

-- 迁移完成
