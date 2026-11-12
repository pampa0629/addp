BEGIN;

-- Historical Transfer executions each had exactly one result. Generated columns
-- materialize that known identity without UPDATE of immutable history. Dropping
-- the generation expression permits independently committed future results.
ALTER TABLE system.engine_access_initial_grant_outcomes
 ADD COLUMN result_id uuid GENERATED ALWAYS AS (execution_id) STORED;
ALTER TABLE system.engine_access_initial_grant_outcomes ALTER COLUMN result_id DROP EXPRESSION;
ALTER TABLE system.engine_access_initial_grant_outcomes ALTER COLUMN result_id SET NOT NULL;
ALTER TABLE system.engine_access_initial_grant_outcomes ADD CONSTRAINT initial_production_result_nonzero
 CHECK(result_id<>'00000000-0000-0000-0000-000000000000');
ALTER TABLE system.engine_access_grants ADD COLUMN production_result_id uuid
 GENERATED ALWAYS AS (CASE WHEN approval_mode='task_production' THEN production_execution_id ELSE NULL END) STORED;
ALTER TABLE system.engine_access_grants ALTER COLUMN production_result_id DROP EXPRESSION;
ALTER TABLE system.engine_access_grants DROP CONSTRAINT source_grant_initial_outcome;
ALTER TABLE system.engine_access_initial_grant_outcomes DROP CONSTRAINT engine_access_initial_grant_outcomes_pkey;
DO $$ DECLARE constraint_name text; BEGIN
 FOR constraint_name IN SELECT conname FROM pg_constraint WHERE conrelid='system.engine_access_initial_grant_outcomes'::regclass AND contype='u' LOOP
  EXECUTE format('ALTER TABLE system.engine_access_initial_grant_outcomes DROP CONSTRAINT %I',constraint_name);
 END LOOP;
END $$;
ALTER TABLE system.engine_access_initial_grant_outcomes
 DROP CONSTRAINT engine_access_initial_grant_outcomes_producer_owner_check,
 ADD CONSTRAINT initial_production_registered_owner_shape CHECK(producer_owner ~ '^[a-z][a-z0-9_]*$'),
 ADD PRIMARY KEY(tenant_id,producer_owner,execution_id,result_id),
 ADD UNIQUE(tenant_id,producer_owner,execution_id,result_id,grant_request_id);
ALTER TABLE system.engine_access_grants ADD CONSTRAINT source_grant_initial_outcome
 FOREIGN KEY(tenant_id,producer_owner,production_execution_id,production_result_id,request_id)
 REFERENCES system.engine_access_initial_grant_outcomes(tenant_id,producer_owner,execution_id,result_id,grant_request_id)
 DEFERRABLE INITIALLY DEFERRED;
DROP INDEX system.idx_source_grant_production;
CREATE UNIQUE INDEX idx_source_grant_production ON system.engine_access_grants
 (tenant_id,producer_owner,production_execution_id,production_result_id) WHERE approval_mode='task_production';
ALTER TABLE system.engine_access_grants DROP CONSTRAINT source_grant_production_origin,
 ADD CONSTRAINT source_grant_production_origin CHECK((CASE
 WHEN approval_mode='task_production' THEN producer_owner ~ '^[a-z][a-z0-9_]*$' AND producer_owner IS NOT NULL
 AND production_execution_id IS NOT NULL AND production_result_id IS NOT NULL
 AND production_result_id<>'00000000-0000-0000-0000-000000000000' AND upload_id IS NULL
 AND issuer_principal_id IS NOT NULL AND issuer_membership_id IS NOT NULL AND recipient_type='user' AND recipient_id=operator_principal_id
 WHEN approval_mode='upload_creation' THEN producer_owner IS NULL AND production_execution_id IS NULL AND production_result_id IS NULL
 AND upload_id IS NOT NULL AND issuer_principal_id IS NOT NULL AND issuer_membership_id IS NOT NULL AND recipient_type='user'
 AND recipient_id=operator_principal_id AND expiry_mode='until_revoked' AND expires_at IS NULL AND node_recursive IS NULL AND scope_boundary IS NULL
 ELSE producer_owner IS NULL AND production_execution_id IS NULL AND production_result_id IS NULL AND upload_id IS NULL
 AND issuer_principal_id IS NULL AND issuer_membership_id IS NULL END) IS TRUE);

CREATE OR REPLACE FUNCTION system.guard_initial_source_grant() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM system.engine_access_initial_grant_outcomes o
  WHERE o.tenant_id=NEW.tenant_id AND o.producer_owner=NEW.producer_owner
   AND o.execution_id=NEW.production_execution_id AND o.result_id=NEW.production_result_id AND o.outcome='issued' AND o.grant_request_id=NEW.request_id
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
   AND s.name='addp-'||NEW.producer_owner AND s.owner_scope='platform' AND c.client_id='addp-'||NEW.producer_owner AND c.status='active'
   AND m.id=NEW.issuer_membership_id AND m.tenant_id=NEW.tenant_id AND m.status='active'
   AND m.joined_at<=NEW.granted_at AND (m.expires_at IS NULL OR m.expires_at>NEW.granted_at)
 AND EXISTS(SELECT 1 FROM system.permissions permission WHERE permission.permission_key=NEW.producer_owner||'.production_receipt.read'
 AND permission.owner_module=NEW.producer_owner AND permission.status='active' AND NOT permission.delegable AND NOT permission.tenant_customizable)) THEN
  RAISE EXCEPTION 'initial grant requires its current producer runtime' USING ERRCODE='23514', CONSTRAINT='initial_source_grant_issuer';
 END IF;
 RETURN NEW;
END;
$$;

COMMIT;
