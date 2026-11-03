BEGIN;
-- Immutable continuation history, never an ACL or a task definition.
CREATE TABLE system.engine_access_upload_grant_outcomes (
 tenant_id bigint NOT NULL REFERENCES system.tenants(id),
 upload_id uuid NOT NULL CHECK(upload_id<>'00000000-0000-0000-0000-000000000000'),
 item_fingerprint text NOT NULL CHECK(item_fingerprint ~ '^[0-9a-f]{64}$'),
 outcome text NOT NULL CHECK(outcome IN ('issued','denied')),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(recorded_at)),
 grant_request_id uuid REFERENCES system.engine_access_grants(request_id) DEFERRABLE INITIALLY DEFERRED,
 denial_code text,
 issuer_principal_id bigint NOT NULL REFERENCES system.principals(id),
 issuer_membership_id bigint NOT NULL REFERENCES system.tenant_memberships(id),
 PRIMARY KEY(tenant_id,upload_id,item_fingerprint),
 UNIQUE(tenant_id,upload_id,item_fingerprint,grant_request_id),
 CHECK(((outcome='issued' AND grant_request_id IS NOT NULL AND denial_code IS NULL) OR
 (outcome='denied' AND grant_request_id IS NULL AND denial_code IN ('not_new','boundary_ineligible','target_history_exists','uploader_inactive','engine_changed'))) IS TRUE)
);
CREATE INDEX idx_upload_outcome_grant ON system.engine_access_upload_grant_outcomes(grant_request_id);
CREATE INDEX idx_upload_outcome_issuer ON system.engine_access_upload_grant_outcomes(issuer_principal_id);
CREATE INDEX idx_upload_outcome_membership ON system.engine_access_upload_grant_outcomes(issuer_membership_id);
CREATE TRIGGER trg_upload_outcome_immutable BEFORE UPDATE OR DELETE ON system.engine_access_upload_grant_outcomes FOR EACH ROW EXECUTE FUNCTION system.preserve_engine_access_grant();
CREATE TRIGGER trg_upload_outcome_no_truncate BEFORE TRUNCATE ON system.engine_access_upload_grant_outcomes FOR EACH STATEMENT EXECUTE FUNCTION system.preserve_engine_access_grant();
ALTER TABLE system.engine_access_grants ADD COLUMN upload_id uuid,
 ADD CONSTRAINT source_grant_upload_outcome FOREIGN KEY(tenant_id,upload_id,item_fingerprint,request_id)
 REFERENCES system.engine_access_upload_grant_outcomes(tenant_id,upload_id,item_fingerprint,grant_request_id) DEFERRABLE INITIALLY DEFERRED,
 DROP CONSTRAINT engine_access_grant_origin,
 ADD CONSTRAINT engine_access_grant_origin CHECK(
 (approval_mode='catalog' AND catalog_request_id=request_id AND catalog_request_id IS NOT NULL AND reason IS NULL) OR
 (approval_mode='independent' AND catalog_request_id IS NULL AND reason IS NOT NULL AND reason=btrim(reason) AND length(reason) BETWEEN 1 AND 2000) OR
 (approval_mode IN ('task_production','upload_creation') AND catalog_request_id IS NULL AND reason IS NULL)),
 DROP CONSTRAINT source_grant_production_origin,
 ADD CONSTRAINT source_grant_production_origin CHECK((CASE
 WHEN approval_mode='task_production' THEN producer_owner='transfer' AND producer_owner IS NOT NULL AND production_execution_id IS NOT NULL AND upload_id IS NULL AND issuer_principal_id IS NOT NULL AND issuer_membership_id IS NOT NULL AND recipient_type='user' AND recipient_id=operator_principal_id
 WHEN approval_mode='upload_creation' THEN producer_owner IS NULL AND production_execution_id IS NULL AND upload_id IS NOT NULL AND issuer_principal_id IS NOT NULL AND issuer_membership_id IS NOT NULL AND recipient_type='user' AND recipient_id=operator_principal_id AND expiry_mode='until_revoked' AND expires_at IS NULL AND node_recursive IS NULL AND scope_boundary IS NULL
 ELSE producer_owner IS NULL AND production_execution_id IS NULL AND upload_id IS NULL AND issuer_principal_id IS NULL AND issuer_membership_id IS NULL END) IS TRUE);
CREATE INDEX idx_source_grant_upload ON system.engine_access_grants(tenant_id,upload_id,item_fingerprint,request_id) WHERE upload_id IS NOT NULL;
ALTER TABLE system.engine_access_grants
 DROP CONSTRAINT source_grant_identity,
 ADD CONSTRAINT source_grant_identity CHECK ((CASE
 WHEN metadata_target IS NOT NULL THEN action='metadata_read' AND approval_mode IN ('independent','upload_creation') AND node_recursive IS NULL
   AND file_composition IS NULL AND scope_boundary IS NULL AND boundary_hash ~ '^[0-9a-f]{64}$'
   AND system.valid_source_metadata_target(metadata_target,catalog_path,engine_id,item_fingerprint)
 WHEN node_recursive IS NULL THEN system.valid_source_single_identity(catalog_path,engine_id,item_fingerprint) AND boundary_hash ~ '^[0-9a-f]{64}$'
 ELSE approval_mode='independent' AND item_fingerprint='' AND boundary_hash='' AND file_composition IS NULL
   AND scope_boundary IS NULL AND system.valid_source_node_path(catalog_path,engine_id) END) IS TRUE),
 DROP CONSTRAINT source_grant_file_composition,
 ADD CONSTRAINT source_grant_file_composition CHECK ((CASE WHEN metadata_target IS NOT NULL THEN file_composition IS NULL
 WHEN system.valid_source_file_path(catalog_path,engine_id) OR system.valid_source_object_path(catalog_path,engine_id)
 THEN approval_mode IN ('independent','upload_creation') AND file_composition IS NOT NULL AND system.valid_source_file_composition(file_composition,catalog_path,engine_id)
 ELSE file_composition IS NULL END) IS TRUE);

CREATE FUNCTION system.guard_upload_source_grant() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM system.engine_access_upload_grant_outcomes o WHERE o.tenant_id=NEW.tenant_id AND o.upload_id=NEW.upload_id AND o.item_fingerprint=NEW.item_fingerprint AND o.outcome='issued' AND o.grant_request_id=NEW.request_id AND o.issuer_principal_id=NEW.issuer_principal_id AND o.issuer_membership_id=NEW.issuer_membership_id) THEN
 RAISE EXCEPTION 'upload grant requires atomic owner outcome' USING ERRCODE='23514',CONSTRAINT='upload_source_grant_outcome'; END IF;
 IF NOT EXISTS(SELECT 1 FROM system.principals p JOIN system.users u ON u.id=p.id JOIN system.tenant_memberships m ON m.principal_id=p.id JOIN system.tenants t ON t.id=m.tenant_id JOIN system.engines e ON e.tenant_id=m.tenant_id AND e.id=NEW.engine_id WHERE p.id=NEW.operator_principal_id AND p.principal_type='user' AND p.status='active' AND p.authorization_version=NEW.operator_authorization_version AND m.id=NEW.operator_membership_id AND m.tenant_id=NEW.tenant_id AND m.status='active' AND m.joined_at<=NEW.granted_at AND (m.expires_at IS NULL OR m.expires_at>NEW.granted_at) AND t.status='active' AND e.engine_type='nfs' AND e.lifecycle_state='active') THEN
 RAISE EXCEPTION 'upload grant requires current original user and NFS engine' USING ERRCODE='23514',CONSTRAINT='upload_source_grant_origin'; END IF;
 IF NOT EXISTS(SELECT 1 FROM system.principals p JOIN system.service_principals s ON s.id=p.id JOIN system.tenant_memberships m ON m.principal_id=p.id JOIN system.oauth_clients c ON c.service_principal_id=p.id WHERE p.id=NEW.issuer_principal_id AND p.principal_type='service_principal' AND p.status='active' AND s.name='addp-manager' AND s.owner_scope='platform' AND c.client_id='addp-manager' AND c.status='active' AND m.id=NEW.issuer_membership_id AND m.tenant_id=NEW.tenant_id AND m.status='active' AND m.joined_at<=NEW.granted_at AND (m.expires_at IS NULL OR m.expires_at>NEW.granted_at)) THEN
 RAISE EXCEPTION 'upload grant requires current Manager runtime' USING ERRCODE='23514',CONSTRAINT='upload_source_grant_issuer'; END IF;
 RETURN NEW;
END;$$;
CREATE TRIGGER trg_upload_source_grant AFTER INSERT ON system.engine_access_grants FOR EACH ROW WHEN(NEW.approval_mode='upload_creation') EXECUTE FUNCTION system.guard_upload_source_grant();
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES('manager.upload_receipt.read','manager','read','low',false,ARRAY['tenant']::text[],false,'permissions.manager.upload_receipt.read.name','permissions.manager.upload_receipt.read.description','active');
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
 SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON
 (p.permission_key='manager.upload_receipt.read' AND r.role_key IN ('tenant.meta_runtime','tenant.system_runtime')) OR
 (p.permission_key='system.engine_access_initial_grant.execute' AND r.role_key='tenant.manager_runtime') WHERE r.tenant_id IS NULL;
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now() WHERE f.revoked_at IS NULL AND EXISTS(SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id WHERE a.principal_id=f.principal_id AND a.status='active' AND r.tenant_id IS NULL AND r.role_key IN ('tenant.manager_runtime','tenant.meta_runtime','tenant.system_runtime'));
COMMIT;
