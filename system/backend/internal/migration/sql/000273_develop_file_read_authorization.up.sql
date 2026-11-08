BEGIN;

-- Add a single sealed purpose to the existing EA. No permission, Role,
-- Assignment, Grant or Meta admission changes are made by this migration.
CREATE FUNCTION system.valid_develop_file_scope(scope jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT SET search_path=pg_catalog AS $$
DECLARE source jsonb; target jsonb; engine bigint;
BEGIN
 IF jsonb_typeof(scope) IS DISTINCT FROM 'object'
  OR NOT scope ?& ARRAY['schema_version','intent_digest','expected_attempt','timeout_seconds','sources']
  OR scope-ARRAY['schema_version','intent_digest','expected_attempt','timeout_seconds','sources','target']<>'{}'::jsonb
  OR scope->>'schema_version' IS DISTINCT FROM 'develop.geopackage-bound-scope/v1'
  OR jsonb_typeof(scope->'intent_digest') IS DISTINCT FROM 'string'
  OR scope->>'intent_digest' !~ '^[0-9a-f]{64}$'
  OR scope->'expected_attempt' IS DISTINCT FROM '1'::jsonb
  OR jsonb_typeof(scope->'timeout_seconds') IS DISTINCT FROM 'number'
  OR scope->>'timeout_seconds' !~ '^[1-9][0-9]*$'
  OR (scope->>'timeout_seconds')::numeric>9223372036
  OR jsonb_typeof(scope->'sources') IS DISTINCT FROM 'array'
  OR jsonb_array_length(scope->'sources')=0 THEN RETURN false; END IF;
 FOR source IN SELECT value FROM jsonb_array_elements(scope->'sources') LOOP
  IF jsonb_typeof(source) IS DISTINCT FROM 'object'
   OR NOT source ?& ARRAY['target','item_fingerprint','boundary_hash','engine_version']
   OR source-ARRAY['target','item_fingerprint','boundary_hash','engine_version']<>'{}'::jsonb
   OR jsonb_typeof(source->'item_fingerprint') IS DISTINCT FROM 'string'
   OR jsonb_typeof(source->'boundary_hash') IS DISTINCT FROM 'string'
   OR source->>'boundary_hash' !~ '^[0-9a-f]{64}$'
   OR jsonb_typeof(source->'engine_version') IS DISTINCT FROM 'string'
   OR source->>'engine_version' !~ '^[1-9][0-9]{0,18}$'
   OR (source->>'engine_version')::numeric>9223372036854775807 THEN RETURN false; END IF;
  engine:=(source->'target'->>'engine_id')::bigint;
  IF system.valid_source_file_path(source->'target',engine) IS NOT TRUE
   OR system.valid_source_single_identity(source->'target',engine,source->>'item_fingerprint') IS NOT TRUE THEN RETURN false; END IF;
 END LOOP;
 IF (SELECT count(DISTINCT value->'target') FROM jsonb_array_elements(scope->'sources'))<>jsonb_array_length(scope->'sources')
  OR (SELECT count(DISTINCT value->>'item_fingerprint') FROM jsonb_array_elements(scope->'sources'))<>jsonb_array_length(scope->'sources') THEN RETURN false; END IF;
 IF scope ? 'target' THEN
  target:=scope->'target';
  IF jsonb_typeof(target) IS DISTINCT FROM 'object' OR NOT target ?& ARRAY['target','mode','engine_version']
   OR target-ARRAY['target','mode','engine_version']<>'{}'::jsonb OR target->>'mode' IS DISTINCT FROM 'fail'
   OR jsonb_typeof(target->'engine_version') IS DISTINCT FROM 'string'
   OR target->>'engine_version' !~ '^[1-9][0-9]{0,18}$'
   OR (target->>'engine_version')::numeric>9223372036854775807 THEN RETURN false; END IF;
  engine:=(target->'target'->>'engine_id')::bigint;
  IF system.valid_source_file_path(target->'target',engine) IS NOT TRUE THEN RETURN false; END IF;
 END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END;
$$;

ALTER TABLE system.execution_authorizations ADD COLUMN develop_file_scope jsonb,
 ADD CONSTRAINT execution_authorizations_develop_file_scope_check CHECK (
  develop_file_scope IS NULL OR ((audience='develop' AND source_type='user'
   AND source_read_scope IS NULL AND internal_task IS NULL AND cleanup_scope IS NULL
   AND source_definition_id IS NULL AND source_definition_version IS NULL
   AND source_notebook_session_authorization_id IS NULL
   AND source_execution_attempt IS NULL AND source_execution_lease_token IS NULL
   AND system.valid_develop_file_scope(develop_file_scope)) IS TRUE));

CREATE FUNCTION system.guard_develop_file_authorization() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.develop_file_scope IS DISTINCT FROM OLD.develop_file_scope THEN
  RAISE EXCEPTION 'Develop file scope is immutable' USING ERRCODE='23514';
 END IF;
 IF NEW.develop_file_scope IS NULL THEN RETURN NEW; END IF;
 IF TG_OP='INSERT' AND NOT EXISTS (
  SELECT 1 FROM common.task_executions e
  WHERE e.execution_id=NEW.execution_id::text AND e.tenant_id=NEW.tenant_id
   AND e.module='develop' AND e.source='develop' AND e.task_type='workflow'
   AND e.execution_boundary='bounded' AND e.trigger_type='manual' AND e.max_attempts=1
   AND e.parent_execution_id IS NULL AND e.retry_of_execution_id IS NULL
   AND e.actor_principal_id=NEW.actor_principal_id AND e.triggered_by=NEW.actor_principal_id
   AND e.actor_tenant_membership_id=NEW.tenant_membership_id
   AND e.issued_authorization_version=NEW.issued_authorization_version
   AND e.status='pending' AND e.attempt=0 AND e.started_at IS NULL
   AND e.execution_authorization_id IS NULL AND e.authorization_expires_at IS NULL
   AND e.lease_token IS NULL AND e.lease_owner IS NULL AND e.lease_expires_at IS NULL
  FOR SHARE OF e
 ) THEN RAISE EXCEPTION 'Develop file authorization requires exact pending execution' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' AND OLD.sealed_at IS NULL AND NEW.sealed_at IS NOT NULL AND (
  EXISTS (SELECT 1 FROM system.execution_authorization_engine_accesses a WHERE a.authorization_id=NEW.id
   AND (a.effects IS DISTINCT FROM ARRAY['read']::text[] OR NOT EXISTS (
    SELECT 1 FROM jsonb_array_elements(NEW.develop_file_scope->'sources') s
    WHERE (s->'target'->>'engine_id')::bigint=a.engine_id)))
  OR EXISTS (SELECT 1 FROM jsonb_array_elements(NEW.develop_file_scope->'sources') s
   WHERE NOT EXISTS (SELECT 1 FROM system.execution_authorization_engine_accesses a
    WHERE a.authorization_id=NEW.id AND a.engine_id=(s->'target'->>'engine_id')::bigint AND a.effects=ARRAY['read']::text[]))
 ) THEN RAISE EXCEPTION 'Develop file authorization requires exact source engines and read only' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER trg_develop_file_authorization BEFORE INSERT OR UPDATE ON system.execution_authorizations
 FOR EACH ROW EXECUTE FUNCTION system.guard_develop_file_authorization();

CREATE FUNCTION system.guard_develop_file_engine_access() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE scope jsonb;
BEGIN
 SELECT develop_file_scope INTO scope FROM system.execution_authorizations WHERE id=NEW.authorization_id;
 IF scope IS NOT NULL AND (NEW.effects IS DISTINCT FROM ARRAY['read']::text[] OR NOT EXISTS (
  SELECT 1 FROM jsonb_array_elements(scope->'sources') source
  WHERE (source->'target'->>'engine_id')::bigint=NEW.engine_id
 )) THEN RAISE EXCEPTION 'Develop file scope is not engine-wide access' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER trg_develop_file_engine_access BEFORE INSERT ON system.execution_authorization_engine_accesses
 FOR EACH ROW EXECUTE FUNCTION system.guard_develop_file_engine_access();

COMMIT;
