-- 回滚：重命名 field_mappings 表回 data_mappings

ALTER TABLE transfer.field_mappings RENAME TO data_mappings;
