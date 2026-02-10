-- 删除 scripts 相关表
-- 这些表功能与 dev_items 重叠，统一使用 dev_items 管理

-- 删除依赖关系表
DROP TABLE IF EXISTS develop.script_dependencies;

-- 删除版本历史表
DROP TABLE IF EXISTS develop.script_versions;

-- 删除主表
DROP TABLE IF EXISTS develop.scripts;

-- 验证删除
-- SELECT table_name FROM information_schema.tables
-- WHERE table_schema = 'develop' AND table_name LIKE 'script%';
