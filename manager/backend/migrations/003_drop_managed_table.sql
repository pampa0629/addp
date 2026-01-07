-- 删除 managed_table 表，统一使用 Meta 模块管理元数据
-- 原因: 与 Meta 模块功能重叠，Manager 和 Meta 存储重复的表元数据
-- 改为: Manager 通过 Meta API 获取表元数据，统一元数据管理
-- 删除时间: 2026-01-07

DROP TABLE IF EXISTS manager.managed_table;
