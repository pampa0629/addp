BEGIN;

-- One Grant store, with an explicit metadata-only boundary. Existing content
-- rows, native hashes, permissions, node scopes and expiries are unchanged.
ALTER TABLE system.engine_access_grants ADD COLUMN metadata_target jsonb;

CREATE FUNCTION system.valid_source_metadata_target(metadata jsonb, path jsonb, engine bigint, fingerprint text)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT SET search_path=pg_catalog AS $$
DECLARE segments jsonb; part jsonb; names text[] := ARRAY[]::text[]; leaf text; separator text; i integer;
BEGIN
 IF jsonb_typeof(metadata) IS DISTINCT FROM 'object'
 OR NOT metadata ?& ARRAY['target','layout','data_type','format','composition_digest']
 OR metadata-ARRAY['target','layout','data_type','format','composition_digest']<>'{}'::jsonb
 OR metadata->'target' IS DISTINCT FROM path OR jsonb_typeof(metadata->'layout') IS DISTINCT FROM 'string' OR metadata->>'layout' NOT IN ('single','multi','whole')
 OR jsonb_typeof(metadata->'data_type') IS DISTINCT FROM 'string' OR octet_length(metadata->>'data_type')>64
 OR jsonb_typeof(metadata->'format') IS DISTINCT FROM 'string' OR octet_length(metadata->>'format')>64
 OR jsonb_typeof(metadata->'composition_digest') IS DISTINCT FROM 'string' OR metadata->>'composition_digest' !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 IF jsonb_typeof(path) IS DISTINCT FROM 'object' OR path-ARRAY['version','engine_id','segments']<>'{}'::jsonb
 OR path->>'version' IS DISTINCT FROM 'catalog.path/v1' OR path->>'engine_id' IS DISTINCT FROM engine::text OR engine<=0
 OR jsonb_typeof(path->'segments') IS DISTINCT FROM 'array' THEN RETURN false; END IF;
 segments:=path->'segments';
 IF jsonb_array_length(segments)<2 OR jsonb_array_length(segments)>64
 OR jsonb_typeof(segments->0) IS DISTINCT FROM 'object' OR (segments->0)-ARRAY['term','kind','name']<>'{}'::jsonb
 OR NOT (segments->0) ?& ARRAY['term','kind','name'] OR jsonb_typeof(segments->0->'term') IS DISTINCT FROM 'string'
 OR jsonb_typeof(segments->0->'kind') IS DISTINCT FROM 'string'
 OR segments->0->>'term' NOT IN ('server','service','root') OR segments->0->>'kind' IS DISTINCT FROM segments->0->>'term'
 OR segments->0->>'name' IS DISTINCT FROM '' THEN RETURN false; END IF;
 leaf:=segments->-1->>'term';
 IF leaf NOT IN ('table','collection','graph','index','keyspace','topic','file','object','directory','bucket','prefix')
 OR NOT (segments->-1->>'kind'=leaf OR (leaf='table' AND segments->-1->>'kind' IN ('view','materialized_view','external_table'))) OR ((leaf IN ('directory','bucket','prefix')) IS DISTINCT FROM (metadata->>'layout'='whole')) OR (leaf NOT IN ('file','object','directory','bucket','prefix') AND metadata->>'layout'<>'single') THEN RETURN false; END IF;
 FOR i IN 1..jsonb_array_length(segments)-1 LOOP
  part:=segments->i;
  IF jsonb_typeof(part) IS DISTINCT FROM 'object' OR NOT part ?& ARRAY['term','kind','name']
  OR jsonb_typeof(part->'term') IS DISTINCT FROM 'string' OR jsonb_typeof(part->'kind') IS DISTINCT FROM 'string' OR part-ARRAY['term','kind','name']<>'{}'::jsonb
  OR jsonb_typeof(part->'name') IS DISTINCT FROM 'string' OR length(part->>'name')=0 OR octet_length(part->>'name')>4096 THEN RETURN false; END IF;
  IF i<jsonb_array_length(segments)-1 AND NOT (
    (part->>'term' IN ('schema','database','catalog') AND part->>'kind'='namespace') OR
    (part->>'term' IN ('directory','bucket','prefix') AND part->>'kind'=part->>'term')) THEN RETURN false; END IF;
  names:=array_append(names,part->>'name');
 END LOOP;
 separator:=CASE WHEN leaf IN ('file','object','directory','bucket','prefix') THEN '/' ELSE '.' END;
 RETURN octet_length(array_to_string(names,separator))<=4096 AND fingerprint=encode(sha256(convert_to(engine::text||':'||array_to_string(names,separator),'UTF8')),'hex');
END;
$$;

ALTER TABLE system.engine_access_grants
 DROP CONSTRAINT source_grant_identity,
 ADD CONSTRAINT source_grant_identity CHECK ((CASE
 WHEN metadata_target IS NOT NULL THEN action='metadata_read' AND approval_mode='independent' AND node_recursive IS NULL
   AND file_composition IS NULL AND scope_boundary IS NULL AND boundary_hash ~ '^[0-9a-f]{64}$'
   AND system.valid_source_metadata_target(metadata_target,catalog_path,engine_id,item_fingerprint)
 WHEN node_recursive IS NULL THEN system.valid_source_single_identity(catalog_path,engine_id,item_fingerprint) AND boundary_hash ~ '^[0-9a-f]{64}$'
 ELSE approval_mode='independent' AND item_fingerprint='' AND boundary_hash='' AND file_composition IS NULL
   AND scope_boundary IS NULL AND system.valid_source_node_path(catalog_path,engine_id) END) IS TRUE),
 DROP CONSTRAINT source_grant_file_composition,
 ADD CONSTRAINT source_grant_file_composition CHECK ((CASE WHEN metadata_target IS NOT NULL THEN file_composition IS NULL
 WHEN system.valid_source_file_path(catalog_path,engine_id) OR system.valid_source_object_path(catalog_path,engine_id)
 THEN approval_mode='independent' AND file_composition IS NOT NULL AND system.valid_source_file_composition(file_composition,catalog_path,engine_id)
 ELSE file_composition IS NULL END) IS TRUE);

ALTER TABLE system.engine_access_grants DROP CONSTRAINT source_grant_whole_scope,
 ADD CONSTRAINT source_grant_whole_scope CHECK ((CASE WHEN metadata_target IS NOT NULL THEN scope_boundary IS NULL
 WHEN system.valid_source_whole_scope_path(catalog_path,engine_id) THEN approval_mode='independent' AND scope_boundary IS NOT NULL AND file_composition IS NULL
  AND scope_boundary=jsonb_build_object('format','filegdb','rule_version',1,'root',catalog_path)
 ELSE scope_boundary IS NULL END) IS TRUE);

COMMIT;
