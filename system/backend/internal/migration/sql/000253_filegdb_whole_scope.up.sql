BEGIN;

-- Exact whole FileGDB identity, never an arbitrary directory-prefix ACL.
CREATE FUNCTION system.valid_source_whole_scope_path(path jsonb, engine bigint)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT SET search_path = pg_catalog AS $$
DECLARE last_part jsonb; file_path jsonb;
BEGIN
 IF jsonb_typeof(path->'segments') IS DISTINCT FROM 'array' OR jsonb_array_length(path->'segments') < 2 THEN RETURN false; END IF;
 last_part := path->'segments'->-1;
 IF last_part->>'term' IS DISTINCT FROM 'directory' OR last_part->>'kind' IS DISTINCT FROM 'directory'
   OR lower(last_part->>'name') NOT LIKE '%.gdb' THEN RETURN false; END IF;
 file_path := jsonb_set(path, '{segments,-1}', last_part || '{"term":"file","kind":"file"}'::jsonb);
 RETURN system.valid_source_file_path(file_path, engine);
END;
$$;

CREATE OR REPLACE FUNCTION system.valid_source_single_identity(path jsonb, engine bigint, fingerprint text)
RETURNS boolean LANGUAGE sql IMMUTABLE STRICT SET search_path = pg_catalog AS $$
 SELECT CASE WHEN system.valid_source_file_path(path, engine) OR system.valid_source_object_path(path, engine) OR system.valid_source_whole_scope_path(path, engine) THEN
  fingerprint ~ '^[0-9a-f]{64}$' AND fingerprint = encode(sha256(convert_to(engine::text || ':' ||
    (SELECT string_agg(value->>'name', '/' ORDER BY ordinality)
     FROM jsonb_array_elements(path->'segments') WITH ORDINALITY WHERE ordinality > 1), 'UTF8')), 'hex')
 ELSE (jsonb_typeof(path) = 'object' AND path->>'version' = 'catalog.path/v1'
 AND path->>'engine_id' = engine::text AND engine > 0
 AND jsonb_typeof(path->'segments') = 'array' AND jsonb_array_length(path->'segments') = 3
 AND path->'segments'->0->>'term' = 'server' AND path->'segments'->0->>'kind' = 'server'
 AND path->'segments'->0->>'name' = '' AND path->'segments'->1->>'kind' = 'namespace'
 AND ((path->'segments'->1->>'term' IN ('schema','database') AND path->'segments'->2->>'term' = 'table' AND path->'segments'->2->>'kind' = 'table')
   OR (path->'segments'->1->>'term' = 'database' AND path->'segments'->2->>'term' = 'collection' AND path->'segments'->2->>'kind' = 'collection'))
 AND length(path->'segments'->1->>'name') > 0 AND length(path->'segments'->2->>'name') > 0
 AND fingerprint ~ '^[0-9a-f]{64}$' AND fingerprint = encode(sha256(convert_to(engine::text || ':' ||
   (path->'segments'->1->>'name') || '.' || (path->'segments'->2->>'name'), 'UTF8')), 'hex')) IS TRUE END
$$;

ALTER TABLE system.engine_access_grants ADD COLUMN scope_boundary jsonb,
 ADD CONSTRAINT source_grant_whole_scope CHECK ((
  CASE WHEN system.valid_source_whole_scope_path(catalog_path, engine_id) THEN
    approval_mode = 'independent' AND scope_boundary IS NOT NULL AND file_composition IS NULL
    AND scope_boundary = jsonb_build_object('format','filegdb','rule_version',1,'root',catalog_path)
  ELSE scope_boundary IS NULL END) IS TRUE);

COMMIT;
