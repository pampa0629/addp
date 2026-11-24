CREATE TRIGGER IF NOT EXISTS source_grant_node_target
BEFORE INSERT ON engine_access_grants FOR EACH ROW
BEGIN
    DECLARE parts, part JSON;
    DECLARE engine_kind, segment_term, segment_kind VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
    DECLARE segment_name LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
    DECLARE segment_count, pos_index INT;
    IF NEW.node_recursive IS NOT NULL THEN
        SET parts=JSON_EXTRACT(NEW.catalog_path,'$.segments'), segment_count=JSON_LENGTH(parts);
        IF (JSON_TYPE(NEW.catalog_path)='OBJECT' AND JSON_LENGTH(NEW.catalog_path)=3
            AND JSON_EXTRACT(NEW.catalog_path,'$.version')=CAST('"catalog.path/v1"' AS JSON)
            AND JSON_TYPE(JSON_EXTRACT(NEW.catalog_path,'$.engine_id'))='INTEGER'
            AND JSON_EXTRACT(NEW.catalog_path,'$.engine_id')=NEW.engine_id
            AND JSON_TYPE(parts)='ARRAY' AND segment_count BETWEEN 1 AND 64) IS NOT TRUE THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='source_grant_node_path';
        END IF;
        SELECT engine_type INTO engine_kind FROM engines WHERE tenant_id=NEW.tenant_id AND id=NEW.engine_id;
        CASE engine_kind
            WHEN 'postgresql' THEN SET segment_term='server';
            WHEN 'nfs' THEN SET segment_term='root';
            WHEN 'minio' THEN SET segment_term='service';
            ELSE SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='source_grant_node_engine';
        END CASE;
        IF JSON_EXTRACT(parts,'$[0]')<>JSON_OBJECT('term',segment_term,'kind',segment_term,'name','')
            OR (engine_kind='postgresql' AND segment_count>2)
            OR (engine_kind='minio' AND segment_count=1 AND NEW.node_recursive=0) THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='source_grant_node_engine';
        END IF;
        SET pos_index=1;
        WHILE pos_index<segment_count DO
            SET part=JSON_EXTRACT(parts,CONCAT('$[',pos_index,']')),
                segment_name=JSON_UNQUOTE(JSON_EXTRACT(part,'$.name'));
            SET segment_term=CASE engine_kind WHEN 'postgresql' THEN 'schema' WHEN 'nfs' THEN 'directory'
                ELSE IF(pos_index=1,'bucket','prefix') END;
            SET segment_kind=IF(engine_kind='postgresql','namespace',segment_term);
            IF (JSON_TYPE(JSON_EXTRACT(part,'$.name'))='STRING'
                AND part=JSON_OBJECT('term',segment_term,'kind',segment_kind,'name',segment_name)
                AND OCTET_LENGTH(segment_name) BETWEEN 1 AND IF(engine_kind='postgresql',512,4096)
                AND (engine_kind='postgresql' OR (segment_name NOT IN ('.','..')
                    AND LOCATE('/',segment_name)=0 AND LOCATE(CHAR(92),segment_name)=0))) IS NOT TRUE THEN
                SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='source_grant_node_path';
            END IF;
            SET pos_index=pos_index+1;
        END WHILE;
    END IF;
END
