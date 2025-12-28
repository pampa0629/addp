-- 重命名表名：local_resources -> local_engines
ALTER TABLE IF EXISTS transfer.local_resources RENAME TO local_engines;

-- 重命名字段：resource_type -> engine_type (保持列名不变，只在应用层改名)
-- 注意：数据库列名保持为 resource_type 以兼容现有数据
-- 应用层代码会使用 `column:resource_type` 标签映射到 EngineType 字段
