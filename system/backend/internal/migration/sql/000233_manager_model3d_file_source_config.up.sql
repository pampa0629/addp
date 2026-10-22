BEGIN;

-- Replace the one pending-config admission rule. Applied migration 000230 is
-- immutable; no legacy version is accepted for new authorizations.
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
     AND e.execution_config->>'config_version'='model3d-glb-file-config/v1'
     AND e.execution_config->'owner_task'=jsonb_build_object('id',NEW.source_read_scope->>'owner_task_id','version',NEW.source_read_scope->>'owner_task_version')
     AND e.execution_config->>'source_engine_version'=NEW.source_read_scope->>'engine_version'
     AND e.execution_config->'source_composition'=NEW.source_read_scope->'file_composition'))
  FOR SHARE OF e
 ) THEN RAISE EXCEPTION 'source scope requires the exact pending Manager execution' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$;

COMMIT;
