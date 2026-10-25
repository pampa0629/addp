BEGIN;

-- Admit manifest-led glTF multi compositions through the same exact read boundary.
-- Existing single compositions keep their representation and hashes.
CREATE OR REPLACE FUNCTION system.valid_source_file_composition(composition jsonb, primary_path jsonb, engine bigint)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT SET search_path = pg_catalog AS $$
DECLARE ref jsonb; parent jsonb; prefix jsonb; primary_count integer := 0; total integer;
BEGIN
 IF NOT (system.valid_source_file_path(primary_path, engine) OR system.valid_source_object_path(primary_path, engine)) OR jsonb_typeof(composition) <> 'object'
   OR composition - ARRAY['format','refs'] <> '{}'::jsonb OR composition->>'format' IS NULL OR composition->>'format' NOT IN ('ifc','3ds','dae','obj','stl','glb','gltf')
   OR jsonb_typeof(composition->'refs') IS DISTINCT FROM 'array' THEN RETURN false; END IF;
 total := jsonb_array_length(composition->'refs');
 IF total < 1 OR total > 200 OR (composition->>'format' IN ('ifc','stl','glb') AND total <> 1) THEN RETURN false; END IF;
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
  IF composition->>'format'='gltf' AND (ref->'required'<>'true'::jsonb OR
    (ref->'primary'='true'::jsonb AND ref->>'role'<>'manifest') OR
    (ref->'primary'='false'::jsonb AND ref->>'role' !~ '^(buffer|image)(_[1-9][0-9]*)?$')) THEN RETURN false; END IF;
  IF ref->'primary' = 'true'::jsonb THEN
   primary_count := primary_count + 1;
   IF ref->'required' <> 'true'::jsonb OR ref->'target' IS DISTINCT FROM primary_path
     OR (composition->>'format'='glb' AND ref->>'role' IS DISTINCT FROM 'model') THEN RETURN false; END IF;
  END IF;
 END LOOP;
 IF composition->>'format'='gltf' AND (SELECT count(DISTINCT value->>'role') <> total FROM jsonb_array_elements(composition->'refs')) THEN RETURN false; END IF;
 RETURN primary_count = 1 AND (SELECT count(DISTINCT value->'target') = total FROM jsonb_array_elements(composition->'refs'));
END;
$$;

CREATE OR REPLACE FUNCTION system.valid_manager_model3d_scope(scope jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT SET search_path=pg_catalog AS $$
DECLARE key text; primary_path jsonb; engine bigint;
BEGIN
 IF jsonb_typeof(scope)<>'object' OR NOT scope ?& ARRAY['config_digest','owner_task_id','owner_task_version','engine_version','file_composition']
 OR scope-ARRAY['config_digest','owner_task_id','owner_task_version','engine_version','file_composition']<>'{}'::jsonb
 OR jsonb_typeof(scope->'config_digest') IS DISTINCT FROM 'string' OR scope->>'config_digest' !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 FOREACH key IN ARRAY ARRAY['owner_task_id','owner_task_version','engine_version'] LOOP
  IF jsonb_typeof(scope->key) IS DISTINCT FROM 'string' OR scope->>key !~ '^[1-9][0-9]{0,18}$'
     OR (scope->>key)::numeric>9223372036854775807 THEN RETURN false; END IF;
 END LOOP;
 engine:=system.manager_source_engine_id(scope);
 SELECT ref->'target' INTO primary_path FROM jsonb_array_elements(
  CASE WHEN jsonb_typeof(scope->'file_composition'->'refs')='array' THEN scope->'file_composition'->'refs' ELSE '[]'::jsonb END) ref
 WHERE ref->'primary'='true'::jsonb LIMIT 1;
 RETURN scope->'file_composition'->>'format' IN ('ifc','3ds','dae','obj','stl','gltf') AND engine IS NOT NULL AND primary_path IS NOT NULL AND system.valid_source_file_composition(scope->'file_composition',primary_path,engine) IS TRUE;
END;
$$;

COMMIT;
