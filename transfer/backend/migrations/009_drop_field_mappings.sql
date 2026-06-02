-- 009_drop_field_mappings.sql
-- 删除旧外层字段映射表。Transfer 字段映射主路径为 transfer_tasks.config.transforms[type=field_mapping]。

DROP TABLE IF EXISTS transfer.field_mappings CASCADE;
DROP TABLE IF EXISTS transfer.data_mappings CASCADE;
