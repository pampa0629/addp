BEGIN;

-- A terminal continuation result, not a second ACL or task registry. Transient
-- failures leave no row. Success and its one source Grant commit together.
CREATE TABLE system.engine_access_initial_grant_outcomes (
 tenant_id bigint NOT NULL REFERENCES system.tenants(id),
 producer_owner text NOT NULL CHECK (producer_owner = 'transfer'),
 execution_id uuid NOT NULL CHECK (execution_id <> '00000000-0000-0000-0000-000000000000'),
 outcome text NOT NULL CHECK (outcome IN ('issued','denied')),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK (isfinite(recorded_at)),
 grant_request_id uuid REFERENCES system.engine_access_grants(request_id) DEFERRABLE INITIALLY DEFERRED,
 denial_code text,
 issuer_principal_id bigint NOT NULL REFERENCES system.principals(id),
 issuer_membership_id bigint NOT NULL REFERENCES system.tenant_memberships(id),
 PRIMARY KEY (tenant_id, producer_owner, execution_id),
 UNIQUE (tenant_id, producer_owner, execution_id, grant_request_id),
 CHECK (((outcome='issued' AND grant_request_id IS NOT NULL AND denial_code IS NULL)
  OR (outcome='denied' AND grant_request_id IS NULL AND denial_code IN
   ('production_ineligible','source_access_denied','execution_permission_denied','target_history_exists','engine_changed'))) IS TRUE)
);
CREATE TRIGGER trg_initial_grant_outcome_immutable BEFORE UPDATE OR DELETE
 ON system.engine_access_initial_grant_outcomes FOR EACH ROW EXECUTE FUNCTION system.preserve_engine_access_grant();
CREATE TRIGGER trg_initial_grant_outcome_no_truncate BEFORE TRUNCATE
 ON system.engine_access_initial_grant_outcomes FOR EACH STATEMENT EXECUTE FUNCTION system.preserve_engine_access_grant();

ALTER TABLE system.engine_access_grants
 ADD COLUMN producer_owner text,
 ADD COLUMN production_execution_id uuid,
 ADD COLUMN issuer_principal_id bigint REFERENCES system.principals(id),
 ADD COLUMN issuer_membership_id bigint REFERENCES system.tenant_memberships(id),
 ADD CONSTRAINT source_grant_initial_outcome FOREIGN KEY
  (tenant_id, producer_owner, production_execution_id, request_id) REFERENCES
  system.engine_access_initial_grant_outcomes(tenant_id, producer_owner, execution_id, grant_request_id)
  DEFERRABLE INITIALLY DEFERRED,
 DROP CONSTRAINT engine_access_grant_origin,
 DROP CONSTRAINT engine_access_grant_provenance,
 ADD CONSTRAINT engine_access_grant_origin CHECK (
  (approval_mode='catalog' AND catalog_request_id=request_id AND catalog_request_id IS NOT NULL AND reason IS NULL)
  OR (approval_mode='independent' AND catalog_request_id IS NULL AND reason IS NOT NULL AND reason=btrim(reason) AND length(reason) BETWEEN 1 AND 2000)
  OR (approval_mode='task_production' AND catalog_request_id IS NULL AND reason IS NULL AND NOT initialized_approval)),
 ADD CONSTRAINT engine_access_grant_provenance CHECK (operator_authorization_version>0 AND
  ((approval_mode='task_production' AND requirement_version=0) OR (approval_mode<>'task_production' AND requirement_version>0))),
 ADD CONSTRAINT source_grant_production_origin CHECK (
  (approval_mode='task_production' AND producer_owner='transfer' AND producer_owner IS NOT NULL
   AND production_execution_id IS NOT NULL AND issuer_principal_id IS NOT NULL AND issuer_membership_id IS NOT NULL
   AND recipient_type='user' AND recipient_id=operator_principal_id)
  OR (approval_mode<>'task_production' AND producer_owner IS NULL AND production_execution_id IS NULL
   AND issuer_principal_id IS NULL AND issuer_membership_id IS NULL));
CREATE UNIQUE INDEX idx_source_grant_production ON system.engine_access_grants(tenant_id,producer_owner,production_execution_id)
 WHERE approval_mode='task_production';

-- Existing Catalog/direct guards and relation de-duplication remain untouched.
-- This source-specific guard still runs only inside the one Grant insert path.
CREATE FUNCTION system.guard_initial_source_grant() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM system.engine_access_initial_grant_outcomes o
  WHERE o.tenant_id=NEW.tenant_id AND o.producer_owner=NEW.producer_owner
   AND o.execution_id=NEW.production_execution_id AND o.outcome='issued' AND o.grant_request_id=NEW.request_id
   AND o.issuer_principal_id=NEW.issuer_principal_id AND o.issuer_membership_id=NEW.issuer_membership_id) THEN
  RAISE EXCEPTION 'initial grant requires its atomic terminal outcome' USING ERRCODE='23514', CONSTRAINT='initial_source_grant_outcome';
 END IF;
 IF NOT EXISTS (SELECT 1 FROM system.principals p JOIN system.users u ON u.id=p.id
  JOIN system.tenant_memberships m ON m.principal_id=p.id JOIN system.tenants t ON t.id=m.tenant_id
  WHERE p.id=NEW.operator_principal_id AND p.principal_type='user' AND p.status='active'
   AND p.authorization_version=NEW.operator_authorization_version AND m.id=NEW.operator_membership_id
   AND m.tenant_id=NEW.tenant_id AND m.status='active' AND m.joined_at<=NEW.granted_at
   AND (m.expires_at IS NULL OR m.expires_at>NEW.granted_at) AND t.status='active') THEN
  RAISE EXCEPTION 'initial grant requires its current human origin' USING ERRCODE='23514', CONSTRAINT='initial_source_grant_origin';
 END IF;
 IF NOT EXISTS (SELECT 1 FROM system.principals p JOIN system.service_principals s ON s.id=p.id
  JOIN system.tenant_memberships m ON m.principal_id=p.id JOIN system.oauth_clients c ON c.service_principal_id=p.id
  WHERE p.id=NEW.issuer_principal_id AND p.principal_type='service_principal' AND p.status='active'
   AND s.name='addp-transfer' AND s.owner_scope='platform' AND c.client_id='addp-transfer' AND c.status='active'
   AND m.id=NEW.issuer_membership_id AND m.tenant_id=NEW.tenant_id AND m.status='active'
   AND m.joined_at<=NEW.granted_at AND (m.expires_at IS NULL OR m.expires_at>NEW.granted_at)) THEN
  RAISE EXCEPTION 'initial grant requires its current producer runtime' USING ERRCODE='23514', CONSTRAINT='initial_source_grant_issuer';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER trg_initial_source_grant AFTER INSERT ON system.engine_access_grants
 FOR EACH ROW WHEN (NEW.approval_mode='task_production') EXECUTE FUNCTION system.guard_initial_source_grant();

INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,
 tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES ('system.engine_access_initial_grant.execute','system','execute','high',false,ARRAY['tenant']::text[],false,
 'permissions.system.engine_access_initial_grant.execute.name','permissions.system.engine_access_initial_grant.execute.description','active');
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
 SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key='system.engine_access_initial_grant.execute'
 WHERE r.tenant_id IS NULL AND r.role_key='tenant.transfer_runtime';
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
 WHERE f.revoked_at IS NULL AND EXISTS (SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
  WHERE a.principal_id=f.principal_id AND a.status='active' AND r.tenant_id IS NULL AND r.role_key='tenant.transfer_runtime');

COMMIT;
