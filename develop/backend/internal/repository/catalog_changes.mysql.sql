CREATE TRIGGER IF NOT EXISTS dev_task_catalog_insert
AFTER INSERT ON dev_tasks FOR EACH ROW
BEGIN
    IF NEW.dev_type IN ('query','workflow') THEN
        INSERT INTO catalog_resource_changes (tenant_id,source_type,source_identity,operation,snapshot,observed_at)
        VALUES (NEW.tenant_id,'dev_task',NEW.id,IF(NEW.deleted_at IS NULL,'upsert','missing'),
            JSON_MERGE_PATCH(JSON_OBJECT(),JSON_OBJECT(
                'name',COALESCE(NULLIF(TRIM(NEW.display_name),''),NEW.name),
                'code',NEW.name,'object_kind','development_task','artifact_type',NEW.dev_type,'task_status',NEW.status,
                'query_type',IF(NEW.dev_type='query',JSON_EXTRACT(NEW.content,'$.query_type'),NULL),
                'engine_id',IF(JSON_UNQUOTE(JSON_EXTRACT(NEW.execution_config,'$.engine_id')) REGEXP '^[1-9][0-9]*$',JSON_UNQUOTE(JSON_EXTRACT(NEW.execution_config,'$.engine_id')),NULL)
            )),CURRENT_TIMESTAMP(6));
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS dev_task_catalog_update
AFTER UPDATE ON dev_tasks FOR EACH ROW
BEGIN
    IF NEW.dev_type IN ('query','workflow') THEN
        INSERT INTO catalog_resource_changes (tenant_id,source_type,source_identity,operation,snapshot,observed_at)
        VALUES (NEW.tenant_id,'dev_task',NEW.id,IF(NEW.deleted_at IS NULL,'upsert','missing'),
            JSON_MERGE_PATCH(JSON_OBJECT(),JSON_OBJECT(
                'name',COALESCE(NULLIF(TRIM(NEW.display_name),''),NEW.name),
                'code',NEW.name,'object_kind','development_task','artifact_type',NEW.dev_type,'task_status',NEW.status,
                'query_type',IF(NEW.dev_type='query',JSON_EXTRACT(NEW.content,'$.query_type'),NULL),
                'engine_id',IF(JSON_UNQUOTE(JSON_EXTRACT(NEW.execution_config,'$.engine_id')) REGEXP '^[1-9][0-9]*$',JSON_UNQUOTE(JSON_EXTRACT(NEW.execution_config,'$.engine_id')),NULL)
            )),CURRENT_TIMESTAMP(6));
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS dev_task_catalog_delete
AFTER DELETE ON dev_tasks FOR EACH ROW
BEGIN
    IF OLD.dev_type IN ('query','workflow') THEN
        INSERT INTO catalog_resource_changes (tenant_id,source_type,source_identity,operation,snapshot,observed_at)
        VALUES (OLD.tenant_id,'dev_task',OLD.id,'missing',
            JSON_MERGE_PATCH(JSON_OBJECT(),JSON_OBJECT(
                'name',COALESCE(NULLIF(TRIM(OLD.display_name),''),OLD.name),
                'code',OLD.name,'object_kind','development_task','artifact_type',OLD.dev_type,'task_status',OLD.status,
                'query_type',IF(OLD.dev_type='query',JSON_EXTRACT(OLD.content,'$.query_type'),NULL),
                'engine_id',IF(JSON_UNQUOTE(JSON_EXTRACT(OLD.execution_config,'$.engine_id')) REGEXP '^[1-9][0-9]*$',JSON_UNQUOTE(JSON_EXTRACT(OLD.execution_config,'$.engine_id')),NULL)
            )),CURRENT_TIMESTAMP(6));
    END IF;
END$$
