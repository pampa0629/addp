BEGIN;

-- Extend the existing read-only parent chain. No historical record, permission,
-- role, source Grant or already-applied migration is rewritten.
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
    (e.source='orchestrator' AND NOT NEW.develop_file_scope ? 'target' AND EXISTS (
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
    (e.source='orchestrator' AND NOT NEW.develop_file_scope ? 'target' AND EXISTS (
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
