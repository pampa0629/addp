-- 将 meta_node.scan_status 从中文值迁移为英文枚举值
-- 旧值：未扫描/扫描中/已扫描/扫描失败
-- 新值：pending/running/completed/failed

BEGIN;

UPDATE metadata.meta_node SET scan_status = 'pending'   WHERE scan_status = '未扫描';
UPDATE metadata.meta_node SET scan_status = 'running'   WHERE scan_status = '扫描中';
UPDATE metadata.meta_node SET scan_status = 'completed' WHERE scan_status = '已扫描';
UPDATE metadata.meta_node SET scan_status = 'failed'    WHERE scan_status = '扫描失败';

-- 修改列默认值
ALTER TABLE metadata.meta_node ALTER COLUMN scan_status SET DEFAULT 'pending';

COMMIT;
