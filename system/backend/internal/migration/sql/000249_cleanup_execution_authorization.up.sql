BEGIN;
-- Publish only the reviewed purposes. Graph reconciliation gets no role grant.
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,
 tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES
 ('system.cleanup_authorization.execute','system','execute','high',false,ARRAY['tenant']::text[],false,
  'permissions.system.cleanup_authorization.execute.name','permissions.system.cleanup_authorization.execute.description','active'),
 ('graph.graph.reconcile','graph','reconcile','high',false,ARRAY['tenant']::text[],true,
  'permissions.graph.graph.reconcile.name','permissions.graph.graph.reconcile.description','active');
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
 SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key='system.cleanup_authorization.execute'
 WHERE r.tenant_id IS NULL AND r.role_key IN ('tenant.asset_runtime','tenant.develop_runtime','tenant.graph_runtime',
 'tenant.manager_runtime','tenant.meta_runtime','tenant.model_runtime','tenant.quality_runtime','tenant.service_runtime',
 'tenant.standard_runtime','tenant.transfer_runtime');
UPDATE system.refresh_token_families f SET revoked_at=statement_timestamp(),revoked_reason='authorization_catalog_changed',updated_at=statement_timestamp()
 WHERE f.revoked_at IS NULL AND EXISTS (SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
  WHERE a.principal_id=f.principal_id AND a.status='active' AND r.tenant_id IS NULL AND r.role_key IN
  ('tenant.asset_runtime','tenant.develop_runtime','tenant.graph_runtime','tenant.manager_runtime','tenant.meta_runtime',
   'tenant.model_runtime','tenant.quality_runtime','tenant.service_runtime','tenant.standard_runtime','tenant.transfer_runtime'));

ALTER TABLE system.execution_authorizations
 ADD COLUMN cleanup_scope jsonb,
 DROP CONSTRAINT execution_authorizations_audience_check,
 ADD CONSTRAINT execution_authorizations_audience_check CHECK (audience IN ('model','quality','develop','transfer','service','duckdb','ontology','manager','cleanup')),
 ADD CONSTRAINT execution_authorizations_cleanup_scope_check CHECK (
  (cleanup_scope IS NULL AND audience <> 'cleanup') OR ((
   audience='cleanup' AND source_type='user' AND internal_task IS NULL AND source_read_scope IS NULL
   AND source_definition_id IS NULL AND source_definition_version IS NULL
   AND source_notebook_session_authorization_id IS NULL AND source_execution_attempt IS NULL AND source_execution_lease_token IS NULL
   AND jsonb_typeof(cleanup_scope)='object'
   AND cleanup_scope ?& ARRAY['request_id','parent_execution_id','request_digest','module']
   AND cleanup_scope-ARRAY['request_id','parent_execution_id','request_digest','module']='{}'::jsonb
   AND jsonb_typeof(cleanup_scope->'request_id')='string'
   AND jsonb_typeof(cleanup_scope->'parent_execution_id')='string'
   AND jsonb_typeof(cleanup_scope->'request_digest')='string'
   AND jsonb_typeof(cleanup_scope->'module')='string'
   AND cleanup_scope->>'request_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   AND cleanup_scope->>'parent_execution_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   AND cleanup_scope->>'request_id'<>'00000000-0000-0000-0000-000000000000'
   AND cleanup_scope->>'parent_execution_id'<>'00000000-0000-0000-0000-000000000000'
   AND cleanup_scope->>'request_digest' ~ '^[0-9a-f]{64}$'
   AND cleanup_scope->>'module' IN ('asset','develop','graph','manager','meta','model','quality','service','standard','transfer')
  ) IS TRUE)
 );

CREATE OR REPLACE FUNCTION system.validate_execution_authorization_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.cleanup_scope IS DISTINCT FROM OLD.cleanup_scope
       OR NEW.internal_task IS DISTINCT FROM OLD.internal_task
       OR NEW.actor_principal_id IS DISTINCT FROM OLD.actor_principal_id
       OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.tenant_membership_id IS DISTINCT FROM OLD.tenant_membership_id
       OR NEW.issued_authorization_version IS DISTINCT FROM OLD.issued_authorization_version
       OR NEW.source_type IS DISTINCT FROM OLD.source_type
       OR NEW.source_definition_id IS DISTINCT FROM OLD.source_definition_id
       OR NEW.source_definition_version IS DISTINCT FROM OLD.source_definition_version
       OR NEW.source_notebook_session_authorization_id IS DISTINCT FROM OLD.source_notebook_session_authorization_id
       OR NEW.source_execution_attempt IS DISTINCT FROM OLD.source_execution_attempt
       OR NEW.source_execution_lease_token IS DISTINCT FROM OLD.source_execution_lease_token
       OR NEW.execution_id IS DISTINCT FROM OLD.execution_id
       OR NEW.audience IS DISTINCT FROM OLD.audience
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'execution authorization identity and boundary are immutable'
            USING ERRCODE = '23514';
    END IF;

    IF OLD.sealed_at IS NULL AND NEW.sealed_at IS NOT NULL THEN
        IF NEW.sealed_at IS DISTINCT FROM NEW.created_at
           OR (NEW.internal_task IS NULL AND NEW.cleanup_scope IS NULL AND NOT EXISTS (
                SELECT 1
                FROM system.execution_authorization_engine_accesses access
                WHERE access.authorization_id = NEW.id
           )) THEN
            RAISE EXCEPTION 'execution authorization sealing requires a non-empty immutable access boundary'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.sealed_at IS DISTINCT FROM OLD.sealed_at THEN
        RAISE EXCEPTION 'execution authorization seal is immutable'
            USING ERRCODE = '23514';
    END IF;

    IF OLD.revoked_at IS NOT NULL
       AND (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
            OR NEW.revoked_reason IS DISTINCT FROM OLD.revoked_reason) THEN
        RAISE EXCEPTION 'execution authorization revocation is immutable'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;


CREATE FUNCTION system.validate_cleanup_execution_authorization() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p record; c record; request jsonb;
BEGIN
 IF NEW.cleanup_scope IS NULL THEN RETURN NEW; END IF;
 SELECT * INTO p FROM common.task_executions WHERE execution_id=NEW.cleanup_scope->>'parent_execution_id' FOR UPDATE;
 SELECT * INTO c FROM common.task_executions WHERE execution_id=NEW.execution_id::text FOR UPDATE;
 request := (c.execution_config->>'cleanup_request')::jsonb;
 IF NOT ((p.module='system' AND p.source='system' AND p.task_type='cleanup' AND p.parent_execution_id IS NULL
  AND p.tenant_id=NEW.tenant_id AND p.status='pending' AND p.attempt=0
  AND c.module=NEW.cleanup_scope->>'module' AND c.source='system' AND c.task_type='cleanup_executor'
  AND c.execution_boundary='bounded' AND c.parent_execution_id=p.execution_id
  AND c.tenant_id=p.tenant_id AND c.status='pending' AND c.attempt=0 AND c.started_at IS NULL
  AND c.lease_token IS NULL AND c.completed_at IS NULL AND c.execution_authorization_id IS NULL
  AND c.actor_principal_id=NEW.actor_principal_id AND c.actor_tenant_membership_id=NEW.tenant_membership_id
  AND c.issued_authorization_version=NEW.issued_authorization_version
  AND c.actor_principal_id=p.actor_principal_id AND c.actor_tenant_membership_id=p.actor_tenant_membership_id
  AND c.issued_authorization_version=p.issued_authorization_version
  AND c.triggered_by=c.actor_principal_id AND p.triggered_by=p.actor_principal_id
  AND c.execution_config->>'cleanup_request'=p.execution_config->>'cleanup_request'
  AND request->>'schema'='addp.cleanup-request/v1'
  AND request->>'request_id'=NEW.cleanup_scope->>'request_id'
  AND request->>'parent_execution_id'=p.execution_id
  AND request->>'digest'=NEW.cleanup_scope->>'request_digest'
  AND (request->>'requested_by')::bigint=NEW.actor_principal_id
  AND (request->>'tenant_id')::bigint=NEW.tenant_id
  AND coalesce(request->>'cause_event','') IN ('','engine.deleting','engine.deleted')
  AND request->'expected_modules' ? c.module) IS TRUE) THEN
  RAISE EXCEPTION 'cleanup authorization requires exact pending parent and owner child' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_cleanup_execution_authorization BEFORE INSERT ON system.execution_authorizations
 FOR EACH ROW EXECUTE FUNCTION system.validate_cleanup_execution_authorization();

CREATE OR REPLACE FUNCTION system.reject_internal_task_engine_access() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM system.execution_authorizations WHERE id=NEW.authorization_id AND (internal_task IS NOT NULL OR cleanup_scope IS NOT NULL)) THEN
  RAISE EXCEPTION 'operation authorization cannot grant engine access' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
COMMIT;
