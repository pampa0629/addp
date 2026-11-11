BEGIN;

-- Preserve Meta TIFF/PAM composition and admit its existing Raster COG purpose.
-- Existing Grants and immutable execution scopes are not rewritten.
CREATE OR REPLACE FUNCTION system.valid_source_file_composition(composition jsonb, primary_path jsonb, engine bigint)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT SET search_path = pg_catalog AS $$
DECLARE ref jsonb; parent jsonb; prefix jsonb; primary_count integer := 0; total integer;
BEGIN
 IF NOT (system.valid_source_file_path(primary_path, engine) OR system.valid_source_object_path(primary_path, engine)) OR jsonb_typeof(composition) <> 'object'
   OR composition - (CASE WHEN composition->>'format'='tiff' THEN ARRAY['format','refs','source_layout'] ELSE ARRAY['format','refs'] END) <> '{}'::jsonb
   OR composition->>'format' IS NULL OR composition->>'format' NOT IN ('ifc','3ds','dae','obj','stl','glb','gltf','fbx','envi','geopackage','tiff')
   OR jsonb_typeof(composition->'refs') IS DISTINCT FROM 'array' THEN RETURN false; END IF;
 total := jsonb_array_length(composition->'refs');
 IF total < 1 OR total > 200 OR (composition->>'format' IN ('ifc','stl','glb','fbx','geopackage') AND total <> 1) THEN RETURN false; END IF;
 IF composition->>'format'='geopackage' AND NOT system.valid_source_file_path(primary_path, engine) THEN RETURN false; END IF;
 IF composition->>'format'='tiff' AND (total > 2 OR
   (composition->>'source_layout' IN ('single','multi')) IS NOT TRUE
   OR lower(primary_path->'segments'->-1->>'name') !~ '\.tiff?$') THEN RETURN false; END IF;
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
  IF composition->>'format'='tiff' AND (
    jsonb_array_length(ref->'target'->'segments')<>jsonb_array_length(primary_path->'segments')
    OR (ref->'primary'='false'::jsonb AND (ref->>'role'<>'auxiliary_metadata'
      OR ref->'target'->'segments'->-1->>'name' IS DISTINCT FROM (primary_path->'segments'->-1->>'name')||'.aux.xml'))) THEN RETURN false; END IF;
  IF composition->>'format'='envi' AND (total<>2 OR ref->'required'<>'true'::jsonb
    OR jsonb_array_length(ref->'target'->'segments')<>jsonb_array_length(primary_path->'segments')
    OR (ref->'primary'='true'::jsonb AND (ref->>'role'<>'raster' OR lower(ref->'target'->'segments'->-1->>'name') ~ '\.hdr$'))
    OR (ref->'primary'='false'::jsonb AND (ref->>'role'<>'header' OR lower(ref->'target'->'segments'->-1->>'name') !~ '\.hdr$'))) THEN RETURN false; END IF;
  IF composition->>'format'='gltf' AND (ref->'required'<>'true'::jsonb OR
    (ref->'primary'='true'::jsonb AND ref->>'role'<>'manifest') OR
    (ref->'primary'='false'::jsonb AND ref->>'role' !~ '^(buffer|image)(_[1-9][0-9]*)?$')) THEN RETURN false; END IF;
  IF ref->'primary' = 'true'::jsonb THEN
   primary_count := primary_count + 1;
   IF ref->'required' <> 'true'::jsonb OR ref->'target' IS DISTINCT FROM primary_path
     OR (composition->>'format' IN ('glb','fbx') AND ref->>'role' IS DISTINCT FROM 'model')
     OR (composition->>'format'='geopackage' AND ref->>'role' IS DISTINCT FROM 'container')
     OR (composition->>'format'='tiff' AND ref->>'role' IS DISTINCT FROM 'image') THEN RETURN false; END IF;
  END IF;
 END LOOP;
 IF composition->>'format'='gltf' AND (SELECT count(DISTINCT value->>'role') <> total FROM jsonb_array_elements(composition->'refs')) THEN RETURN false; END IF;
 RETURN primary_count = 1 AND (SELECT count(DISTINCT value->'target') = total FROM jsonb_array_elements(composition->'refs'));
END;
$$;

CREATE OR REPLACE FUNCTION system.valid_manager_file_scope(scope jsonb)
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
 RETURN scope->'file_composition'->>'format' IN ('ifc','3ds','dae','obj','stl','gltf','fbx','envi','tiff') AND engine IS NOT NULL AND primary_path IS NOT NULL AND system.valid_source_file_composition(scope->'file_composition',primary_path,engine) IS TRUE;
END;
$$;

CREATE OR REPLACE FUNCTION system.validate_execution_source_read_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF NEW.source_read_scope IS DISTINCT FROM OLD.source_read_scope THEN
   RAISE EXCEPTION 'execution source read scope is immutable' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.source_read_scope IS NULL THEN RETURN NEW; END IF;
 IF NEW.audience='transfer' THEN
  IF NOT EXISTS (
   SELECT 1 FROM common.task_executions e
   WHERE e.execution_id=NEW.execution_id::text AND e.tenant_id=NEW.tenant_id
    AND e.module='transfer' AND e.source='manager' AND e.task_type='sync'
    AND e.execution_boundary='bounded' AND e.trigger_type='manual'
    AND e.parent_execution_id IS NULL AND e.source_task_id IS NULL AND e.max_attempts=1
    AND e.status='pending' AND e.attempt=0
    AND e.execution_authorization_id IS NULL AND e.authorization_expires_at IS NULL
    AND e.lease_token IS NULL AND e.lease_owner IS NULL AND e.lease_expires_at IS NULL
    AND e.triggered_by=NEW.actor_principal_id AND e.actor_principal_id=NEW.actor_principal_id
    AND e.actor_tenant_membership_id=NEW.tenant_membership_id
    AND e.issued_authorization_version=NEW.issued_authorization_version
    AND jsonb_typeof(e.metadata->'export_artifact_protection')='object'
    AND e.execution_config->'target'->>'format'='filegdb'
    AND e.execution_config->'filegdb_source'=NEW.source_read_scope->'filegdb_source'
   FOR SHARE OF e
  ) THEN RAISE EXCEPTION 'FileGDB source scope requires the exact pending Transfer execution' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END IF;
 IF NOT EXISTS (
  SELECT 1 FROM common.task_executions e
  WHERE e.execution_id=NEW.execution_id::text AND e.tenant_id=NEW.tenant_id
   AND e.module='manager' AND e.source='manager' AND e.execution_boundary='bounded' AND e.trigger_type='manual'
   AND e.parent_execution_id IS NULL AND e.status='pending' AND e.attempt=0
   AND e.execution_authorization_id IS NULL AND e.authorization_expires_at IS NULL
   AND e.lease_token IS NULL AND e.lease_owner IS NULL AND e.lease_expires_at IS NULL
   AND e.actor_principal_id=NEW.actor_principal_id AND e.actor_tenant_membership_id=NEW.tenant_membership_id
   AND e.issued_authorization_version=NEW.issued_authorization_version
   AND ((e.task_type='data_profiling' AND e.source_task_id IS NULL
     AND e.execution_config->>'config_version'='data-profile-config/v6'
     AND NEW.source_read_scope ? 'read_set'
     AND e.execution_config->'read_set'=NEW.source_read_scope->'read_set')
    OR (system.valid_manager_file_scope(NEW.source_read_scope)
     AND ((e.task_type='model_3d_glb_generation' AND NEW.source_read_scope->'file_composition'->>'format' NOT IN ('envi','tiff') AND e.execution_config->>'config_version'='model3d-glb-file-config/v1')
      OR (e.task_type='raster_cog_generation' AND NEW.source_read_scope->'file_composition'->>'format' IN ('envi','tiff') AND e.execution_config->>'config_version'='raster-cog-file-config/v1'))
     AND e.triggered_by=e.actor_principal_id AND e.source_task_id=NEW.source_read_scope->>'owner_task_id'
     AND e.execution_config->'owner_task'=jsonb_build_object('id',NEW.source_read_scope->>'owner_task_id','version',NEW.source_read_scope->>'owner_task_version')
     AND e.execution_config->>'source_engine_version'=NEW.source_read_scope->>'engine_version'
     AND e.execution_config->'source_composition'=NEW.source_read_scope->'file_composition'))
  FOR SHARE OF e
 ) THEN RAISE EXCEPTION 'source scope requires the exact pending Manager execution' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$;

COMMIT;
