-- 重命名 data_mappings 表为 field_mappings
-- 更准确地表达字段映射的本质

ALTER TABLE transfer.data_mappings RENAME TO field_mappings;
