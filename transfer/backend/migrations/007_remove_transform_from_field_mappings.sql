-- 删除 field_mappings 表的 transform 字段
-- 该字段前端收集但后端从未使用

ALTER TABLE transfer.field_mappings DROP COLUMN IF EXISTS transform;
