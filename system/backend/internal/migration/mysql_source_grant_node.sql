CREATE TRIGGER IF NOT EXISTS source_grant_catalog_node
BEFORE INSERT ON engine_access_grants FOR EACH ROW
BEGIN
    DECLARE parts, part JSON;
    DECLARE root_term, segment_term, segment_kind VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
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
        SET root_term=JSON_UNQUOTE(JSON_EXTRACT(parts,'$[0].term')), segment_term=root_term;
        IF (root_term IN ('server','root','service')) IS NOT TRUE THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='source_grant_node_path';
        END IF;
        IF JSON_EXTRACT(parts,'$[0]')<>JSON_OBJECT('term',segment_term,'kind',segment_term,'name','')
            OR (root_term='server' AND segment_count>2)
            OR (root_term='service' AND segment_count=1 AND NEW.node_recursive=0) THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='source_grant_node_engine';
        END IF;
        SET pos_index=1;
        WHILE pos_index<segment_count DO
            SET part=JSON_EXTRACT(parts,CONCAT('$[',pos_index,']')),
                segment_name=JSON_UNQUOTE(JSON_EXTRACT(part,'$.name'));
            SET segment_term=CASE root_term WHEN 'server' THEN 'schema' WHEN 'root' THEN 'directory'
                ELSE IF(pos_index=1,'bucket','prefix') END;
            SET segment_kind=IF(root_term='server','namespace',segment_term);
            IF (JSON_TYPE(JSON_EXTRACT(part,'$.name'))='STRING'
                AND part=JSON_OBJECT('term',segment_term,'kind',segment_kind,'name',segment_name)
                AND OCTET_LENGTH(segment_name) BETWEEN 1 AND IF(root_term='server',512,4096)
                AND (root_term='server' OR (segment_name NOT IN ('.','..')
                    AND LOCATE('/',segment_name)=0 AND LOCATE(CHAR(92),segment_name)=0))) IS NOT TRUE THEN
                SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='source_grant_node_path';
            END IF;
            SET pos_index=pos_index+1;
        END WHILE;
    END IF;
END
