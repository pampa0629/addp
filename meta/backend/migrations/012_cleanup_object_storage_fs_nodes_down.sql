-- 回滚：从备份表恢复数据（仅在确认需要回滚时执行）
BEGIN;

INSERT INTO metadata.meta_item
SELECT * FROM metadata.meta_item_backup_012
ON CONFLICT (id) DO NOTHING;

INSERT INTO metadata.meta_node
SELECT * FROM metadata.meta_node_backup_012
ON CONFLICT (id) DO NOTHING;

DROP TABLE IF EXISTS metadata.meta_item_backup_012;
DROP TABLE IF EXISTS metadata.meta_node_backup_012;

COMMIT;
