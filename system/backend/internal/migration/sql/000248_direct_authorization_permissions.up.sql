BEGIN;

-- The one-time development retirement precedes this model replacement. Refuse
-- a piecemeal upgrade while an old process has issued new rules in between.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM system.engine_access_grants) THEN
  RAISE EXCEPTION 'stop old authorization writers before direct permission migration';
 END IF;
END $$;

ALTER TABLE system.engine_access_approval_requirements DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_approval_requirements ADD COLUMN allowed boolean;
UPDATE system.engine_access_approval_requirements SET allowed=(mode='independent');
DROP TRIGGER trg_engine_access_approval_requirement_guard ON system.engine_access_approval_requirements;
DROP TRIGGER trg_engine_access_approval_requirement_no_truncate ON system.engine_access_approval_requirements;
DROP FUNCTION system.guard_engine_access_approval_requirement();
ALTER TABLE system.engine_access_approval_requirements RENAME TO engine_access_direct_permissions;
ALTER TABLE system.engine_access_direct_permissions ALTER COLUMN allowed SET NOT NULL,
 DROP COLUMN mode, DROP COLUMN target_digest;
CREATE UNIQUE INDEX uq_engine_direct_permission_identity ON system.engine_access_direct_permissions(tenant_id,engine_id,item_fingerprint);

CREATE FUNCTION system.guard_engine_access_direct_permission() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP IN ('DELETE','TRUNCATE') THEN
  RAISE EXCEPTION 'direct permission cannot be removed' USING ERRCODE='23514';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 THEN RAISE EXCEPTION 'initial direct permission version must be one' USING ERRCODE='23514'; END IF;
 ELSE
  IF (NEW.id,NEW.tenant_id,NEW.engine_id,NEW.catalog_path,NEW.item_fingerprint)
   IS DISTINCT FROM (OLD.id,OLD.tenant_id,OLD.engine_id,OLD.catalog_path,OLD.item_fingerprint)
   OR NEW.version<>OLD.version+1 THEN
   RAISE EXCEPTION 'direct permission identity is immutable; version must advance once' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_engine_access_direct_permission_guard BEFORE INSERT OR UPDATE OR DELETE
 ON system.engine_access_direct_permissions FOR EACH ROW EXECUTE FUNCTION system.guard_engine_access_direct_permission();
CREATE TRIGGER trg_engine_access_direct_permission_no_truncate BEFORE TRUNCATE
 ON system.engine_access_direct_permissions FOR EACH STATEMENT EXECUTE FUNCTION system.guard_engine_access_direct_permission();

ALTER TABLE system.engine_access_grants DROP CONSTRAINT engine_access_grant_origin,
 DROP COLUMN initialized_approval, DROP COLUMN requirement_version,
 ADD COLUMN direct_permission_source text NOT NULL DEFAULT '',
 ADD COLUMN direct_permission_version bigint NOT NULL DEFAULT 0,
 ADD CONSTRAINT engine_access_grant_origin CHECK (
  (approval_mode='catalog' AND catalog_request_id=request_id AND catalog_request_id IS NOT NULL AND reason IS NULL)
  OR (approval_mode='independent' AND catalog_request_id IS NULL AND reason IS NOT NULL AND reason=btrim(reason) AND length(reason) BETWEEN 1 AND 2000)
  OR (approval_mode='task_production' AND catalog_request_id IS NULL AND reason IS NULL)),
 ADD CONSTRAINT source_grant_direct_permission_provenance CHECK(operator_authorization_version>0 AND
  ((approval_mode='independent' AND direct_permission_source IN ('explicit','deployment_default') AND direct_permission_version>0)
   OR (approval_mode IN ('catalog','task_production') AND direct_permission_source='' AND direct_permission_version=0)));

CREATE OR REPLACE FUNCTION system.guard_engine_access_grant_insert() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
DECLARE
    receipt system.engine_access_fulfillment_outcomes%ROWTYPE;
    permit system.engine_access_direct_permissions%ROWTYPE;
    deployment system.module_definitions%ROWTYPE;
BEGIN
    NEW.granted_at := clock_timestamp();
    IF NEW.approval_mode = 'catalog' THEN
        SELECT * INTO receipt FROM system.engine_access_fulfillment_outcomes
        WHERE request_id = NEW.request_id AND outcome = 'accepted';
        IF receipt.deadline IS NULL OR NEW.granted_at < receipt.recorded_at OR NEW.granted_at >= receipt.deadline THEN
            RAISE EXCEPTION 'engine access grant requires an unexpired accepted fulfillment'
                USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_acceptance_window';
        END IF;
        -- Rule facts come from the trusted receipt, never caller overrides.
        NEW.catalog_request_id := receipt.request_id;
        NEW.item_fingerprint := receipt.binding->>'item_fingerprint';
        NEW.boundary_hash := receipt.binding->>'boundary_hash';
        NEW.tenant_id := receipt.tenant_id; NEW.engine_id := receipt.engine_id; NEW.catalog_path := receipt.catalog_path;
        NEW.recipient_type := receipt.binding->>'recipient_type'; NEW.recipient_id := (receipt.binding->>'recipient_id')::bigint;
        NEW.action := receipt.binding->>'action'; NEW.expiry_mode := receipt.expiry_mode; NEW.expires_at := receipt.grant_expires_at;
        NEW.direct_permission_version := 0; NEW.direct_permission_source := '';
        NEW.operator_principal_id := (receipt.binding->'operator'->>'principal_id')::bigint;
        NEW.operator_membership_id := (receipt.binding->'operator'->>'tenant_membership_id')::bigint;
        NEW.operator_authorization_version := (receipt.binding->'operator'->>'authorization_version')::bigint;
        NEW.reason := NULL;
    ELSIF NEW.approval_mode = 'independent' THEN
        -- No synthetic Catalog receipt. The command service must separately
        -- hold current IAM/delegation and the same precise target boundary.
        -- Serialize permit changes and direct issuance even for a bare insert.
        PERFORM pg_advisory_xact_lock(hashtextextended('engine_access_target|' || NEW.tenant_id::text || '|' || NEW.engine_id::text || '|' || NEW.item_fingerprint, 0));
        SELECT * INTO permit FROM system.engine_access_direct_permissions
          WHERE tenant_id=NEW.tenant_id AND engine_id=NEW.engine_id AND item_fingerprint=NEW.item_fingerprint;
        IF FOUND THEN
          IF NOT permit.allowed OR NEW.direct_permission_source <> 'explicit' OR NEW.direct_permission_version <> permit.version THEN
            RAISE EXCEPTION 'direct permission changed or denied' USING ERRCODE='23514', CONSTRAINT='source_grant_direct_permission';
          END IF;
        ELSE
          SELECT * INTO deployment FROM system.module_definitions WHERE module_name='catalog' FOR SHARE;
          IF NOT FOUND OR deployment.enabled OR NEW.direct_permission_source <> 'deployment_default' OR NEW.direct_permission_version <> deployment.version THEN
            RAISE EXCEPTION 'direct permission default unavailable or denied' USING ERRCODE='23514', CONSTRAINT='source_grant_direct_permission';
          END IF;
        END IF;
        IF NOT EXISTS (SELECT 1 FROM system.tenant_memberships m JOIN system.principals p ON p.id = m.principal_id
            JOIN system.tenants t ON t.id = m.tenant_id
            JOIN system.engines e ON e.tenant_id = t.id AND e.id = NEW.engine_id
            WHERE m.id = NEW.operator_membership_id AND p.id = NEW.operator_principal_id AND m.tenant_id = NEW.tenant_id
                AND p.principal_type = 'user' AND p.status = 'active' AND p.authorization_version = NEW.operator_authorization_version
                AND m.status = 'active' AND m.joined_at <= NEW.granted_at AND (m.expires_at IS NULL OR m.expires_at > NEW.granted_at)
                AND t.status = 'active' AND e.lifecycle_state = 'active') THEN
            RAISE EXCEPTION 'independent grant actor must be current in the source tenant'
                USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_actor';
        END IF;
        IF NOT ((NEW.recipient_type = 'user' AND EXISTS (
            SELECT 1 FROM system.tenant_memberships m JOIN system.principals p ON p.id = m.principal_id
            WHERE m.tenant_id = NEW.tenant_id AND p.id = NEW.recipient_id AND p.principal_type = 'user'
                AND p.status = 'active' AND m.status = 'active' AND m.joined_at <= NEW.granted_at
                AND (m.expires_at IS NULL OR m.expires_at > NEW.granted_at)))
            OR (NEW.recipient_type = 'department' AND EXISTS (SELECT 1 FROM system.departments d
                WHERE d.tenant_id = NEW.tenant_id AND d.id = NEW.recipient_id AND d.status = 'active'))
            OR (NEW.recipient_type = 'project_group' AND EXISTS (SELECT 1 FROM system.project_groups g
                WHERE g.tenant_id = NEW.tenant_id AND g.id = NEW.recipient_id AND g.status = 'active'))) THEN
            RAISE EXCEPTION 'independent grant recipient must be current in the source tenant'
                USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_recipient';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;



-- Retire old mutable-configuration permissions. No custom role gains manage.
CREATE TEMP TABLE direct_permission_affected_principals ON COMMIT DROP AS
SELECT DISTINCT a.principal_id FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
WHERE a.status='active' AND ((r.tenant_id IS NULL AND r.role_key IN ('tenant.engine_access_delegation_administrator','tenant.source_data_authorizer'))
 OR EXISTS(SELECT 1 FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id
  WHERE rp.role_id=r.id AND p.permission_key LIKE 'system.engine_access_approval_requirement.%'));
DELETE FROM system.role_permissions rp USING system.permissions p
 WHERE rp.permission_id=p.id AND p.permission_key LIKE 'system.engine_access_approval_requirement.%';
UPDATE system.permissions SET status='disabled',updated_at=now() WHERE permission_key LIKE 'system.engine_access_approval_requirement.%';
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES
 ('system.engine_access_direct_permission.read','system','read','low',false,ARRAY['tenant']::text[],true,
  'permissions.system.engine_access_direct_permission.read.name','permissions.system.engine_access_direct_permission.read.description','active'),
 ('system.engine_access_direct_permission.update','system','update','high',false,ARRAY['tenant']::text[],false,
  'permissions.system.engine_access_direct_permission.update.name','permissions.system.engine_access_direct_permission.update.description','active');
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
 SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON
  (p.permission_key='system.engine_access_direct_permission.read' AND r.role_key IN ('tenant.engine_access_delegation_administrator','tenant.source_data_authorizer'))
  OR(p.permission_key='system.engine_access_direct_permission.update' AND r.role_key='tenant.engine_access_delegation_administrator')
 WHERE r.tenant_id IS NULL;
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
 FROM direct_permission_affected_principals a WHERE f.principal_id=a.principal_id AND f.revoked_at IS NULL;
COMMIT;
