CREATE TRIGGER IF NOT EXISTS develop_file_validate_sources
BEFORE INSERT ON execution_authorizations FOR EACH ROW
source_guard: BEGIN
    DECLARE source_ref, path_value, segments_value, segment_value JSON;
    DECLARE source_index, segment_index, segment_count INT DEFAULT 0;
    DECLARE object_source BOOLEAN;
    DECLARE expected_term VARCHAR(12);
    DECLARE component, path_name LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
    DECLARE fingerprints JSON DEFAULT (JSON_ARRAY());
    IF NEW.develop_file_scope IS NULL THEN LEAVE source_guard; END IF;
    IF NOT JSON_SCHEMA_VALID('{
        "type":"object","additionalProperties":false,
        "required":["schema_version","intent_digest","expected_attempt","timeout_seconds","sources"],
        "properties":{
          "schema_version":{"enum":["develop.file-bound-scope/v2"]},
          "intent_digest":{"type":"string","pattern":"^[0-9a-f]{64}$"},
          "expected_attempt":{"enum":[1]},"timeout_seconds":{"type":"integer","minimum":1,"maximum":9223372036},
          "sources":{"type":"array","minItems":1,"items":{
            "type":"object","additionalProperties":false,"required":["target","format","item_fingerprint","boundary_hash","engine_version"],
            "properties":{"format":{"enum":["geopackage","geojson"]},"item_fingerprint":{"type":"string","pattern":"^[0-9a-f]{64}$"},
              "boundary_hash":{"type":"string","pattern":"^[0-9a-f]{64}$"},"engine_version":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
              "target":{"type":"object","additionalProperties":false,"required":["version","engine_id","segments"],
                "properties":{"version":{"enum":["catalog.path/v1"]},"engine_id":{"type":"integer","minimum":1},
                  "segments":{"type":"array","minItems":2,"maxItems":64,"items":{
                    "type":"object","additionalProperties":false,"required":["term","kind","name"],
                    "properties":{"term":{"type":"string"},"kind":{"type":"string"},"name":{"type":"string"}}
                  }}}}
            }
          }}
        }
    }',NEW.develop_file_scope) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='invalid Develop read-only file scope';
    END IF;
    WHILE source_index<JSON_LENGTH(NEW.develop_file_scope,'$.sources') DO
        SET source_ref=JSON_EXTRACT(NEW.develop_file_scope,CONCAT('$.sources[',source_index,']')),
            path_value=JSON_EXTRACT(source_ref,'$.target'), segments_value=JSON_EXTRACT(path_value,'$.segments'),
            segment_count=JSON_LENGTH(segments_value), object_source=JSON_UNQUOTE(JSON_EXTRACT(source_ref,'$.format'))='geojson',
            segment_index=0,path_name='';
        IF CAST(JSON_UNQUOTE(JSON_EXTRACT(source_ref,'$.engine_version')) AS DECIMAL(20,0))>9223372036854775807
           OR JSON_EXTRACT(path_value,'$.engine_id')>CAST('9223372036854775807' AS JSON)
           OR (object_source AND segment_count<3) THEN
            SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='invalid Develop file engine or path';
        END IF;
        WHILE segment_index<segment_count DO
            SET segment_value=JSON_EXTRACT(segments_value,CONCAT('$[',segment_index,']')),
                component=JSON_UNQUOTE(JSON_EXTRACT(segment_value,'$.name')),
                expected_term=CASE WHEN segment_index=0 THEN IF(object_source,'service','root')
                                   WHEN segment_index=segment_count-1 THEN IF(object_source,'object','file')
                                   WHEN object_source AND segment_index=1 THEN 'bucket' ELSE IF(object_source,'prefix','directory') END;
            IF BINARY JSON_UNQUOTE(JSON_EXTRACT(segment_value,'$.term'))<>BINARY expected_term OR
               BINARY JSON_UNQUOTE(JSON_EXTRACT(segment_value,'$.kind'))<>BINARY expected_term OR
               (segment_index=0 AND OCTET_LENGTH(component)<>0) OR (segment_index>0 AND
               (OCTET_LENGTH(component) NOT BETWEEN 1 AND 4096 OR component IN ('.','..')
                OR INSTR(component,'/')>0 OR INSTR(component,CHAR(92))>0 OR REGEXP_LIKE(component,'[[:cntrl:]]'))) THEN
                SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='invalid Develop file path segment';
            END IF;
            IF segment_index>0 THEN SET path_name=CONCAT(path_name,IF(segment_index>1,'/',''),component); END IF;
            SET segment_index=segment_index+1;
        END WHILE;
        IF OCTET_LENGTH(path_name)>4096 OR BINARY JSON_UNQUOTE(JSON_EXTRACT(source_ref,'$.item_fingerprint'))<>
            BINARY SHA2(CONCAT(JSON_UNQUOTE(JSON_EXTRACT(path_value,'$.engine_id')),':',path_name),256)
           OR JSON_CONTAINS(fingerprints,JSON_EXTRACT(source_ref,'$.item_fingerprint')) THEN
            SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='invalid or duplicate Develop source identity';
        END IF;
        SET fingerprints=JSON_ARRAY_APPEND(fingerprints,'$',JSON_EXTRACT(source_ref,'$.item_fingerprint')), source_index=source_index+1;
    END WHILE;
END
