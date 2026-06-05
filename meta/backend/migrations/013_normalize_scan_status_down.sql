-- 回滚：将 scan_status 从英文值恢复为中文值
BEGIN;

UPDATE meta.meta_node SET scan_status = '未扫描'  WHERE scan_status = 'pending';
UPDATE meta.meta_node SET scan_status = '扫描中'  WHERE scan_status = 'running';
UPDATE meta.meta_node SET scan_status = '已扫描'  WHERE scan_status = 'completed';
UPDATE meta.meta_node SET scan_status = '扫描失败' WHERE scan_status = 'failed';

ALTER TABLE meta.meta_node ALTER COLUMN scan_status SET DEFAULT '未扫描';

COMMIT;
