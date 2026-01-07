-- 删除未使用的 directories 表
-- 该表从未被实际使用，仅有模型定义和文档
-- 删除时间: 2026-01-07
-- 注意: 使用 CASCADE 删除依赖对象（directory_permissions 表和 data_source_stats 视图）

DROP TABLE IF EXISTS manager.directories CASCADE;
