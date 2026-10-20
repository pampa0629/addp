BEGIN;

-- Keep one scope column and one engine-access trigger. A Manager scope is a
-- closed purpose-specific value, never an additional engine-wide ACL.
CREATE FUNCTION system.manager_source_engine_id(scope jsonb)
RETURNS bigint LANGUAGE plpgsql IMMUTABLE STRICT SET search_path=pg_catalog AS $$
DECLARE value text;
BEGIN
 IF scope ? 'read_set' THEN
  value:=scope->'read_set'->'paths'->0->>'engine_id';
 ELSE
  SELECT ref->'target'->>'engine_id' INTO value
  FROM jsonb_array_elements(CASE WHEN jsonb_typeof(scope->'file_composition'->'refs')='array'
       THEN scope->'file_composition'->'refs' ELSE '[]'::jsonb END) ref
  WHERE ref->'primary'='true'::jsonb LIMIT 1;
 END IF;
 IF value IS NULL OR value !~ '^[1-9][0-9]{0,18}$' OR value::numeric>9223372036854775807 THEN RETURN NULL; END IF;
 RETURN value::bigint;
END;
$$;

CREATE FUNCTION system.valid_manager_model3d_scope(scope jsonb)
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
 RETURN engine IS NOT NULL AND primary_path IS NOT NULL AND system.valid_source_file_composition(scope->'file_composition',primary_path,engine) IS TRUE;
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
    OR system.valid_manager_model3d_scope(source_read_scope))
  ) IS TRUE));

CREATE OR REPLACE FUNCTION system.validate_execution_source_read_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF NEW.source_read_scope IS DISTINCT FROM OLD.source_read_scope THEN
   RAISE EXCEPTION 'execution source read scope is immutable' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.audience<>'manager' THEN RETURN NEW; END IF;
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
    OR (e.task_type='model_3d_glb_generation' AND system.valid_manager_model3d_scope(NEW.source_read_scope)
     AND e.triggered_by=e.actor_principal_id AND e.source_task_id=NEW.source_read_scope->>'owner_task_id'
     AND e.execution_config->>'config_version'='model3d-glb-ftp-config/v1'
     AND e.execution_config->'owner_task'=jsonb_build_object('id',NEW.source_read_scope->>'owner_task_id','version',NEW.source_read_scope->>'owner_task_version')
     AND e.execution_config->>'source_engine_version'=NEW.source_read_scope->>'engine_version'
     AND e.execution_config->'source_composition'=NEW.source_read_scope->'file_composition'))
  FOR SHARE OF e
 ) THEN RAISE EXCEPTION 'source scope requires the exact pending Manager execution' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$;

DROP TRIGGER trg_manager_profile_engine_scope ON system.execution_authorization_engine_accesses;
DROP FUNCTION system.validate_manager_profile_engine_scope();
CREATE FUNCTION system.validate_manager_source_engine_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE auth system.execution_authorizations;
BEGIN
 SELECT * INTO auth FROM system.execution_authorizations WHERE id=NEW.authorization_id;
 IF auth.audience='manager' AND (NEW.effects IS DISTINCT FROM ARRAY['read']::text[]
    OR NEW.engine_id IS DISTINCT FROM system.manager_source_engine_id(auth.source_read_scope)) THEN
  RAISE EXCEPTION 'Manager source scope cannot grant other engines or effects' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER trg_manager_source_engine_scope BEFORE INSERT ON system.execution_authorization_engine_accesses
 FOR EACH ROW EXECUTE FUNCTION system.validate_manager_source_engine_scope();

-- The Manager Runtime already has the canonical execute permission. No User,
-- role assignment, source Grant/Deny or connection privilege is introduced.
COMMIT;
