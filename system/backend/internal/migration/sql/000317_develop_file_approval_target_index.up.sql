BEGIN;

-- Existing approvals each refer to the sole (ordinal zero) target of their
-- immutable scope. Keep the original approval, operation and scope digests.
ALTER TABLE system.develop_file_create_approvals
    ADD COLUMN target_index integer NOT NULL DEFAULT 0 CHECK (target_index >= 0),
    DROP CONSTRAINT develop_file_create_approvals_authorization_id_key,
    ADD CONSTRAINT develop_file_create_approvals_authorization_target_key UNIQUE (authorization_id, target_index);

ALTER TABLE system.develop_file_create_approvals ALTER COLUMN target_index DROP DEFAULT;

CREATE OR REPLACE FUNCTION system.valid_develop_file_scope(scope jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT SET search_path=pg_catalog AS $$
DECLARE source jsonb; target jsonb; engine bigint;
BEGIN
 IF jsonb_typeof(scope) IS DISTINCT FROM 'object'
  OR NOT scope ?& ARRAY['schema_version','intent_digest','expected_attempt','timeout_seconds','sources']
  OR scope-ARRAY['schema_version','intent_digest','expected_attempt','timeout_seconds','sources','targets']<>'{}'::jsonb
  OR scope->>'schema_version' IS DISTINCT FROM 'develop.file-bound-scope/v2'
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
   OR NOT source ?& ARRAY['target','format','item_fingerprint','boundary_hash','engine_version']
   OR source-ARRAY['target','format','item_fingerprint','boundary_hash','engine_version']<>'{}'::jsonb
   OR jsonb_typeof(source->'format') IS DISTINCT FROM 'string'
   OR source->>'format' NOT IN ('geopackage','geojson')
   OR jsonb_typeof(source->'item_fingerprint') IS DISTINCT FROM 'string'
   OR jsonb_typeof(source->'boundary_hash') IS DISTINCT FROM 'string'
   OR source->>'boundary_hash' !~ '^[0-9a-f]{64}$'
   OR jsonb_typeof(source->'engine_version') IS DISTINCT FROM 'string'
   OR source->>'engine_version' !~ '^[1-9][0-9]{0,18}$'
   OR (source->>'engine_version')::numeric>9223372036854775807
   OR (scope ? 'targets' AND source->>'format' IS DISTINCT FROM 'geopackage') THEN RETURN false; END IF;
  engine:=(source->'target'->>'engine_id')::bigint;
  IF ((source->>'format'='geopackage' AND system.valid_source_file_path(source->'target',engine)) OR (source->>'format'='geojson' AND system.valid_source_object_path(source->'target',engine))) IS NOT TRUE
   OR system.valid_source_single_identity(source->'target',engine,source->>'item_fingerprint') IS NOT TRUE THEN RETURN false; END IF;
 END LOOP;
 IF (SELECT count(DISTINCT value->'target') FROM jsonb_array_elements(scope->'sources'))<>jsonb_array_length(scope->'sources')
  OR (SELECT count(DISTINCT value->>'item_fingerprint') FROM jsonb_array_elements(scope->'sources'))<>jsonb_array_length(scope->'sources') THEN RETURN false; END IF;
 IF scope ? 'targets' THEN
  IF jsonb_typeof(scope->'targets') IS DISTINCT FROM 'array' OR jsonb_array_length(scope->'targets')=0 THEN RETURN false; END IF;
  IF (SELECT count(DISTINCT value->'target') FROM jsonb_array_elements(scope->'targets'))<>jsonb_array_length(scope->'targets') THEN RETURN false; END IF;
  FOR target IN SELECT value FROM jsonb_array_elements(scope->'targets') LOOP
  IF jsonb_typeof(target) IS DISTINCT FROM 'object' OR NOT target ?& ARRAY['target','mode','engine_version']
   OR target-ARRAY['target','mode','engine_version']<>'{}'::jsonb OR target->>'mode' IS DISTINCT FROM 'fail'
   OR jsonb_typeof(target->'engine_version') IS DISTINCT FROM 'string'
   OR target->>'engine_version' !~ '^[1-9][0-9]{0,18}$'
   OR (target->>'engine_version')::numeric>9223372036854775807 THEN RETURN false; END IF;
  engine:=(target->'target'->>'engine_id')::bigint;
  IF system.valid_source_file_path(target->'target',engine) IS NOT TRUE THEN RETURN false; END IF;
  END LOOP;
 END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END;
$$;


CREATE OR REPLACE FUNCTION system.guard_develop_file_authorization() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND OLD.develop_file_business_deadline IS NOT NULL
  AND NEW.develop_file_business_deadline IS DISTINCT FROM OLD.develop_file_business_deadline THEN
  RAISE EXCEPTION 'Develop business deadline is immutable' USING ERRCODE='23514';
 END IF;
 IF TG_OP='UPDATE' AND NEW.develop_file_scope IS DISTINCT FROM OLD.develop_file_scope THEN
  RAISE EXCEPTION 'Develop file scope is immutable' USING ERRCODE='23514';
 END IF;
 IF NEW.develop_file_scope IS NULL THEN RETURN NEW; END IF;
 IF TG_OP='INSERT' AND system.valid_develop_file_scope(NEW.develop_file_scope) IS NOT TRUE THEN RAISE EXCEPTION 'Invalid sealed Develop file scope' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' AND NOT EXISTS (
  SELECT 1 FROM common.task_executions e
  WHERE e.execution_id=NEW.execution_id::text AND e.tenant_id=NEW.tenant_id
   AND e.module='develop' AND e.task_type='workflow'
   AND e.execution_boundary='bounded' AND e.trigger_type IN ('manual','scheduled') AND e.max_attempts=1
   AND e.retry_of_execution_id IS NULL
   AND ((e.source='develop' AND e.parent_execution_id IS NULL AND e.trigger_type='manual') OR
    (e.source='orchestrator' AND NOT NEW.develop_file_scope ? 'targets' AND EXISTS (
     SELECT 1 FROM common.task_executions p WHERE p.execution_id=e.parent_execution_id AND p.tenant_id=e.tenant_id
      AND p.module='orchestrator' AND p.task_type='orchestration' AND p.status='running' AND p.trigger_type=e.trigger_type AND p.attempt>0
      AND p.lease_token IS NOT NULL AND p.lease_owner IS NOT NULL AND p.lease_owner<>'' AND p.lease_expires_at>clock_timestamp()
      AND p.actor_principal_id=e.actor_principal_id AND p.triggered_by=p.actor_principal_id
      AND p.actor_tenant_membership_id=e.actor_tenant_membership_id AND p.issued_authorization_version=e.issued_authorization_version
     FOR SHARE OF p)))
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
 IF TG_OP='INSERT' AND NEW.develop_file_business_deadline IS NOT NULL THEN
  RAISE EXCEPTION 'Pending authorization has no business deadline' USING ERRCODE='23514';
 END IF;
 IF TG_OP='UPDATE' AND OLD.develop_file_business_deadline IS NULL
  AND NEW.develop_file_business_deadline IS NOT NULL THEN
  IF NEW.sealed_at IS NULL OR NEW.revoked_at IS NOT NULL OR NOT EXISTS (
    SELECT 1 FROM common.task_executions e WHERE e.execution_id=NEW.execution_id::text
     AND e.tenant_id=NEW.tenant_id AND e.execution_authorization_id=NEW.id
     AND e.authorization_expires_at=NEW.expires_at AND e.module='develop'
     AND e.task_type='workflow' AND e.execution_boundary='bounded' AND e.trigger_type IN ('manual','scheduled')
     AND e.retry_of_execution_id IS NULL
   AND ((e.source='develop' AND e.parent_execution_id IS NULL AND e.trigger_type='manual') OR
    (e.source='orchestrator' AND NOT NEW.develop_file_scope ? 'targets' AND EXISTS (
     SELECT 1 FROM common.task_executions p WHERE p.execution_id=e.parent_execution_id AND p.tenant_id=e.tenant_id
      AND p.module='orchestrator' AND p.task_type='orchestration' AND p.status='running' AND p.trigger_type=e.trigger_type AND p.attempt>0
      AND p.lease_token IS NOT NULL AND p.lease_owner IS NOT NULL AND p.lease_owner<>'' AND p.lease_expires_at>clock_timestamp()
      AND p.actor_principal_id=e.actor_principal_id AND p.triggered_by=p.actor_principal_id
      AND p.actor_tenant_membership_id=e.actor_tenant_membership_id AND p.issued_authorization_version=e.issued_authorization_version
     FOR SHARE OF p))) AND e.max_attempts=1
     AND e.actor_principal_id=NEW.actor_principal_id AND e.triggered_by=NEW.actor_principal_id
     AND e.actor_tenant_membership_id=NEW.tenant_membership_id
     AND e.issued_authorization_version=NEW.issued_authorization_version
     AND e.status='running' AND e.attempt=(NEW.develop_file_scope->>'expected_attempt')::integer
     AND e.started_at IS NOT NULL AND e.started_at<=clock_timestamp()
     AND e.lease_token IS NOT NULL AND e.lease_owner IS NOT NULL AND e.lease_owner<>''
     AND e.lease_expires_at>clock_timestamp()
     AND NEW.develop_file_business_deadline>clock_timestamp()
     AND NEW.develop_file_business_deadline<=e.started_at+
       make_interval(secs=>(NEW.develop_file_scope->>'timeout_seconds')::double precision)
    FOR SHARE OF e
   ) THEN RAISE EXCEPTION 'Develop business deadline requires original running execution' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END;
$$;

COMMIT;
