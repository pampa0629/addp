-- 删除 tasks 表的 type 字段
-- 该字段从未在业务逻辑中使用，纯粹是僵尸字段

ALTER TABLE transfer.tasks DROP COLUMN IF EXISTS type;
