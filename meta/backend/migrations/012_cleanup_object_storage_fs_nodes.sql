-- 清理对象存储引擎（minio/s3）下错误写入的文件系统语义节点
-- 根因：历史版本将 ObjectStoragePlugin 路由到 FileSystemScanService，
--       导致写入 node_type=root/dir 和 item_type=file/table，
--       与正确的 bucket/prefix/object 语义混用。
-- 本迁移仅清理对象存储引擎的错误数据，NFS/NAS 的 root/dir/file 不受影响。

BEGIN;

-- 备份受影响的 meta_item（item_type=file/table，挂在对象存储引擎下）
CREATE TABLE IF NOT EXISTS metadata.meta_item_backup_012 AS
SELECT mi.*
FROM metadata.meta_item mi
JOIN metadata.meta_node mn ON mi.node_id = mn.id
JOIN system.engines e ON mn.engine_id = e.id
WHERE e.engine_type IN ('minio', 's3', 'oss', 'object_storage', 'object-storage')
  AND mi.item_type IN ('file', 'table')
  AND mi.deleted_at IS NULL;

-- 备份受影响的 meta_node（node_type=root/dir，属于对象存储引擎）
CREATE TABLE IF NOT EXISTS metadata.meta_node_backup_012 AS
SELECT mn.*
FROM metadata.meta_node mn
JOIN system.engines e ON mn.engine_id = e.id
WHERE e.engine_type IN ('minio', 's3', 'oss', 'object_storage', 'object-storage')
  AND mn.node_type IN ('root', 'dir')
  AND mn.deleted_at IS NULL;

-- 硬删除对象存储引擎下 file/table 类型的 meta_item
DELETE FROM metadata.meta_item
WHERE id IN (SELECT id FROM metadata.meta_item_backup_012);

-- 硬删除对象存储引擎下 root/dir 类型的 meta_node（含软删除记录）
DELETE FROM metadata.meta_node
WHERE id IN (SELECT id FROM metadata.meta_node_backup_012);

COMMIT;
