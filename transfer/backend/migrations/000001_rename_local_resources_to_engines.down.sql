-- 回滚：重命名表名 local_engines -> local_resources
ALTER TABLE IF EXISTS transfer.local_engines RENAME TO local_resources;
