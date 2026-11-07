BEGIN;

-- The existing source_read_scope gains one Transfer purpose for Manager's
-- FileGDB archive. Existing grants and execution authorizations are untouched.
CREATE FUNCTION system.valid_filegdb_export_source_scope(scope jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT SET search_path=pg_catalog AS $$
DECLARE source jsonb; binding jsonb; target jsonb; engine bigint;
BEGIN
 IF jsonb_typeof(scope)<>'object' OR NOT scope ?& ARRAY['config_digest','filegdb_source']
  OR scope-ARRAY['config_digest','filegdb_source']<>'{}'::jsonb
  OR jsonb_typeof(scope->'config_digest') IS DISTINCT FROM 'string'
  OR scope->>'config_digest' !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 source := scope->'filegdb_source';
 IF jsonb_typeof(source) IS DISTINCT FROM 'object' OR NOT source ?& ARRAY['engine_version','binding']
  OR source-ARRAY['engine_version','binding']<>'{}'::jsonb
  OR jsonb_typeof(source->'engine_version') IS DISTINCT FROM 'string'
  OR source->>'engine_version' !~ '^[1-9][0-9]{0,18}$'
  OR (source->>'engine_version')::numeric>9223372036854775807 THEN RETURN false; END IF;
 binding := source->'binding';
 IF jsonb_typeof(binding) IS DISTINCT FROM 'object' OR NOT binding ?& ARRAY['item_fingerprint','boundary_hash','native_target']
  OR binding-ARRAY['item_fingerprint','boundary_hash','native_target']<>'{}'::jsonb
  OR jsonb_typeof(binding->'item_fingerprint') IS DISTINCT FROM 'string'
  OR jsonb_typeof(binding->'boundary_hash') IS DISTINCT FROM 'string'
  OR binding->>'item_fingerprint' !~ '^[0-9a-f]{64}$'
  OR binding->>'boundary_hash' !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 target := binding->'native_target';
 IF jsonb_typeof(target->'engine_id') IS DISTINCT FROM 'number' OR target->>'engine_id' !~ '^[1-9][0-9]{0,18}$'
  OR (target->>'engine_id')::numeric>9223372036854775807 THEN RETURN false; END IF;
 engine := (target->>'engine_id')::bigint;
 RETURN (target->'segments'->1->>'term' IN ('schema','database')
  AND target->'segments'->2->>'term'='table'
  AND system.valid_source_single_identity(target,engine,binding->>'item_fingerprint')) IS TRUE;
END;
$$;

ALTER TABLE system.execution_authorizations DROP CONSTRAINT execution_authorizations_source_read_scope_check,
 ADD CONSTRAINT execution_authorizations_source_read_scope_check CHECK (
  (source_read_scope IS NULL AND audience<>'manager') OR ((
   audience='manager' AND source_type='user' AND internal_task IS NULL
   AND source_notebook_session_authorization_id IS NULL
   AND source_execution_attempt IS NULL AND source_execution_lease_token IS NULL
   AND ((jsonb_typeof(source_read_scope)='object'
     AND source_read_scope ?& ARRAY['config_digest','read_set']
     AND source_read_scope-ARRAY['config_digest','read_set']='{}'::jsonb
     AND jsonb_typeof(source_read_scope->'config_digest')='string'
     AND source_read_scope->>'config_digest' ~ '^[0-9a-f]{64}$'
     AND jsonb_typeof(source_read_scope->'read_set')='object'
     AND (source_read_scope->'read_set')-ARRAY['paths']='{}'::jsonb
     AND jsonb_typeof(source_read_scope->'read_set'->'paths')='array'
     AND jsonb_array_length(source_read_scope->'read_set'->'paths') BETWEEN 1 AND 200)
    OR system.valid_manager_file_scope(source_read_scope))
  ) IS TRUE) OR ((
   audience='transfer' AND source_type='user' AND internal_task IS NULL AND cleanup_scope IS NULL
   AND source_notebook_session_authorization_id IS NULL
   AND source_execution_attempt IS NULL AND source_execution_lease_token IS NULL
   AND system.valid_filegdb_export_source_scope(source_read_scope)
  ) IS TRUE));

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
     AND ((e.task_type='model_3d_glb_generation' AND NEW.source_read_scope->'file_composition'->>'format'<>'envi' AND e.execution_config->>'config_version'='model3d-glb-file-config/v1')
      OR (e.task_type='raster_cog_generation' AND NEW.source_read_scope->'file_composition'->>'format'='envi' AND e.execution_config->>'config_version'='raster-cog-file-config/v1'))
     AND e.triggered_by=e.actor_principal_id AND e.source_task_id=NEW.source_read_scope->>'owner_task_id'
     AND e.execution_config->'owner_task'=jsonb_build_object('id',NEW.source_read_scope->>'owner_task_id','version',NEW.source_read_scope->>'owner_task_version')
     AND e.execution_config->>'source_engine_version'=NEW.source_read_scope->>'engine_version'
     AND e.execution_config->'source_composition'=NEW.source_read_scope->'file_composition'))
  FOR SHARE OF e
 ) THEN RAISE EXCEPTION 'source scope requires the exact pending Manager execution' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION system.validate_manager_source_engine_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE auth system.execution_authorizations; scope_engine bigint;
BEGIN
 SELECT * INTO auth FROM system.execution_authorizations WHERE id=NEW.authorization_id;
 IF auth.source_read_scope IS NULL THEN RETURN NEW; END IF;
 IF auth.audience='transfer' THEN
  scope_engine := (auth.source_read_scope->'filegdb_source'->'binding'->'native_target'->>'engine_id')::bigint;
 ELSE
  scope_engine := system.manager_source_engine_id(auth.source_read_scope);
 END IF;
 IF NEW.effects IS DISTINCT FROM ARRAY['read']::text[] OR NEW.engine_id IS DISTINCT FROM scope_engine THEN
  RAISE EXCEPTION 'source scope cannot grant other engines or effects' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
COMMIT;
