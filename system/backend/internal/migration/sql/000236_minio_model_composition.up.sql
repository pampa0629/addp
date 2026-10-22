BEGIN;

-- Exact object composition extends the existing Grant; no historical grants are synthesized.
CREATE FUNCTION system.valid_source_object_path(path jsonb, engine bigint)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT SET search_path = pg_catalog AS $$
DECLARE segment jsonb; position integer := 0; total integer;
BEGIN
 IF jsonb_typeof(path) <> 'object' OR path - ARRAY['version','engine_id','segments'] <> '{}'::jsonb
    OR path->>'version' IS DISTINCT FROM 'catalog.path/v1' OR path->>'engine_id' IS DISTINCT FROM engine::text
    OR engine <= 0 OR jsonb_typeof(path->'segments') IS DISTINCT FROM 'array' THEN RETURN false; END IF;
 total := jsonb_array_length(path->'segments');
 IF total < 3 OR total > 64 OR path->'segments'->0 IS DISTINCT FROM '{"term":"service","kind":"service","name":""}'::jsonb THEN RETURN false; END IF;
 FOR segment IN SELECT value FROM jsonb_array_elements(path->'segments') LOOP
  position := position + 1;
  IF jsonb_typeof(segment) <> 'object' OR segment - ARRAY['term','kind','name'] <> '{}'::jsonb THEN RETURN false; END IF;
  IF position = 1 THEN CONTINUE; END IF;
  IF jsonb_typeof(segment->'name') IS DISTINCT FROM 'string' OR octet_length(segment->>'name') NOT BETWEEN 1 AND 4096
    OR segment->>'name' IN ('.','..') OR position('/' IN segment->>'name') > 0
    OR position(chr(92) IN segment->>'name') > 0 OR segment->>'name' ~ '[[:cntrl:]]' THEN RETURN false; END IF;
  IF position = total THEN
   IF segment->>'term' IS DISTINCT FROM 'object' OR segment->>'kind' IS DISTINCT FROM 'object' THEN RETURN false; END IF;
  ELSIF position = 2 THEN
   IF segment->>'term' IS DISTINCT FROM 'bucket' OR segment->>'kind' IS DISTINCT FROM 'bucket' THEN RETURN false; END IF;
  ELSIF segment->>'term' IS DISTINCT FROM 'prefix' OR segment->>'kind' IS DISTINCT FROM 'prefix' THEN RETURN false;
  END IF;
 END LOOP;
 RETURN (SELECT octet_length(string_agg(value->>'name', '/' ORDER BY ordinality)) <= 4096
   FROM jsonb_array_elements(path->'segments') WITH ORDINALITY WHERE ordinality > 1);
END;
$$;

CREATE OR REPLACE FUNCTION system.valid_source_single_identity(path jsonb, engine bigint, fingerprint text)
RETURNS boolean LANGUAGE sql IMMUTABLE STRICT SET search_path = pg_catalog AS $$
 SELECT CASE WHEN system.valid_source_file_path(path, engine) OR system.valid_source_object_path(path, engine) THEN
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

CREATE OR REPLACE FUNCTION system.valid_source_file_composition(composition jsonb, primary_path jsonb, engine bigint)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT SET search_path = pg_catalog AS $$
DECLARE ref jsonb; parent jsonb; prefix jsonb; primary_count integer := 0; total integer;
BEGIN
 IF NOT (system.valid_source_file_path(primary_path, engine) OR system.valid_source_object_path(primary_path, engine)) OR jsonb_typeof(composition) <> 'object'
   OR composition - ARRAY['format','refs'] <> '{}'::jsonb OR composition->>'format' IS NULL OR composition->>'format' NOT IN ('ifc','3ds','dae')
   OR jsonb_typeof(composition->'refs') IS DISTINCT FROM 'array' THEN RETURN false; END IF;
 IF system.valid_source_object_path(primary_path, engine) AND composition->>'format' = 'dae' THEN RETURN false; END IF;
 total := jsonb_array_length(composition->'refs');
 IF total < 1 OR total > 200 OR (composition->>'format' = 'ifc' AND total <> 1) THEN RETURN false; END IF;
 SELECT jsonb_agg(value ORDER BY ordinality) INTO parent
 FROM jsonb_array_elements(primary_path->'segments') WITH ORDINALITY
 WHERE ordinality < jsonb_array_length(primary_path->'segments');
 FOR ref IN SELECT value FROM jsonb_array_elements(composition->'refs') LOOP
  IF jsonb_typeof(ref) <> 'object' OR ref - ARRAY['target','role','required','primary'] <> '{}'::jsonb
    OR ref->>'role' IS NULL OR ref->>'role' !~ '^[a-z0-9_]{1,64}$'
    OR jsonb_typeof(ref->'required') IS DISTINCT FROM 'boolean' OR jsonb_typeof(ref->'primary') IS DISTINCT FROM 'boolean'
    OR NOT (system.valid_source_file_path(ref->'target', engine) OR system.valid_source_object_path(ref->'target', engine))
    OR ref->'target'->'segments'->-1->>'term' IS DISTINCT FROM primary_path->'segments'->-1->>'term'
    OR jsonb_array_length(ref->'target'->'segments') < jsonb_array_length(primary_path->'segments') THEN RETURN false; END IF;
  SELECT jsonb_agg(value ORDER BY ordinality) INTO prefix
  FROM jsonb_array_elements(ref->'target'->'segments') WITH ORDINALITY
  WHERE ordinality < jsonb_array_length(primary_path->'segments');
  IF prefix IS DISTINCT FROM parent THEN RETURN false; END IF;
  IF ref->'primary' = 'true'::jsonb THEN
   primary_count := primary_count + 1;
   IF ref->'required' <> 'true'::jsonb OR ref->'target' IS DISTINCT FROM primary_path THEN RETURN false; END IF;
  END IF;
 END LOOP;
 RETURN primary_count = 1 AND (SELECT count(DISTINCT value->'target') = total FROM jsonb_array_elements(composition->'refs'));
END;
$$;

ALTER TABLE system.engine_access_grants DROP CONSTRAINT source_grant_file_composition,
 ADD CONSTRAINT source_grant_file_composition CHECK ((
  CASE WHEN system.valid_source_file_path(catalog_path, engine_id) OR system.valid_source_object_path(catalog_path, engine_id)
   THEN approval_mode = 'independent' AND file_composition IS NOT NULL AND system.valid_source_file_composition(file_composition, catalog_path, engine_id)
   ELSE file_composition IS NULL END) IS TRUE);

COMMIT;
