-- ========================================
-- 数据库迁移脚本：重命名 geopandas 引擎为 python_workflow
-- 创建时间：2025-12-30
-- 说明：将所有 api.geopandas 引用更新为 api.python_workflow
-- ========================================

-- 1. 更新 system.engines 表中的引擎类型
UPDATE system.engines
SET engine_type = 'api.python_workflow',
    name = REPLACE(name, 'geopandas', 'python_workflow'),
    display_name = REPLACE(REPLACE(display_name, 'GeoPandas', 'Python Workflow'), '空间计算', '数据处理工作流')
WHERE engine_type = 'api.geopandas';

-- 2. 验证迁移结果
SELECT
    '迁移完成' as status,
    (SELECT COUNT(*) FROM system.engines WHERE engine_type = 'api.python_workflow') as python_workflow_engines,
    (SELECT COUNT(*) FROM system.engines WHERE engine_type = 'api.geopandas') as geopandas_engines;

-- 预期结果：geopandas_engines 应该为 0
