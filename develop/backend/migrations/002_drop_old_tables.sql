-- 002_drop_old_tables.sql
-- 删除旧的develop表结构

-- 删除旧的执行结果表
DROP TABLE IF EXISTS develop.spatial_execution_results CASCADE;

-- 删除旧的执行记录表
DROP TABLE IF EXISTS develop.spatial_executions CASCADE;

-- 删除旧的空间任务表
DROP TABLE IF EXISTS develop.spatial_tasks CASCADE;

-- 删除旧的SQL执行记录表
DROP TABLE IF EXISTS develop.executions CASCADE;

-- scripts 表暂时保留，未来整合
-- DROP TABLE IF EXISTS develop.scripts CASCADE;

-- 清理可能存在的视图
DROP VIEW IF EXISTS develop.v_spatial_task_summary CASCADE;
DROP VIEW IF EXISTS develop.v_execution_statistics CASCADE;
