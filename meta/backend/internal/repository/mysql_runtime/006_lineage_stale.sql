CREATE TRIGGER IF NOT EXISTS meta_item_stale_lineage
AFTER UPDATE ON meta_item FOR EACH ROW
BEGIN
    IF OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
        UPDATE meta.lineage_item_relations SET status='stale',updated_at=CURRENT_TIMESTAMP(6)
        WHERE tenant_id=NEW.tenant_id AND status<>'closed' AND NEW.id IN (source_item_id,target_item_id);
        UPDATE meta.lineage_service_dependencies SET status='stale',updated_at=CURRENT_TIMESTAMP(6)
        WHERE tenant_id=NEW.tenant_id AND status<>'closed' AND source_item_id=NEW.id;
    ELSEIF NOT (OLD.attributes <=> NEW.attributes) THEN
        IF EXISTS (
            SELECT 1 FROM (
                SELECT path_key,field_type,element_type,nullable_flag,SUM(delta) AS difference
                FROM (
                    SELECT CAST(COALESCE(NULLIF(f.path_key,JSON_ARRAY()),JSON_ARRAY(f.name)) AS CHAR CHARACTER SET utf8mb4) COLLATE utf8mb4_0900_bin AS path_key,
                           f.field_type,f.element_type,COALESCE(f.nullable_flag,0) AS nullable_flag,1 AS delta
                    FROM JSON_TABLE(OLD.attributes,'$.type_info.table.fields[*]' COLUMNS (
                        name LONGTEXT PATH '$.name',path_key JSON PATH '$.path',field_type VARCHAR(64) PATH '$.type',
                        element_type VARCHAR(64) PATH '$.element_type',nullable_flag BOOLEAN PATH '$.nullable')) f
                    UNION ALL
                    SELECT CAST(COALESCE(NULLIF(f.path_key,JSON_ARRAY()),JSON_ARRAY(f.name)) AS CHAR CHARACTER SET utf8mb4) COLLATE utf8mb4_0900_bin,
                           f.field_type,f.element_type,COALESCE(f.nullable_flag,0),-1
                    FROM JSON_TABLE(NEW.attributes,'$.type_info.table.fields[*]' COLUMNS (
                        name LONGTEXT PATH '$.name',path_key JSON PATH '$.path',field_type VARCHAR(64) PATH '$.type',
                        element_type VARCHAR(64) PATH '$.element_type',nullable_flag BOOLEAN PATH '$.nullable')) f
                ) projection GROUP BY path_key,field_type,element_type,nullable_flag HAVING difference<>0
            ) changed
        ) THEN
            UPDATE meta.lineage_item_relations SET status='stale',updated_at=CURRENT_TIMESTAMP(6)
            WHERE tenant_id=NEW.tenant_id AND granularity='field' AND status<>'closed' AND NEW.id IN (source_item_id,target_item_id);
        END IF;
    END IF;
END
