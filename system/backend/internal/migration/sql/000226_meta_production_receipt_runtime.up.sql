BEGIN;

-- Complete the canonical initial-source provenance FK access paths without
-- editing migration 225, which may already have been applied.
CREATE INDEX idx_source_grant_issuer_principal ON system.engine_access_grants(issuer_principal_id);
CREATE INDEX idx_source_grant_issuer_membership ON system.engine_access_grants(issuer_membership_id);
CREATE INDEX idx_initial_outcome_issuer_principal ON system.engine_access_initial_grant_outcomes(issuer_principal_id);
CREATE INDEX idx_initial_outcome_issuer_membership ON system.engine_access_initial_grant_outcomes(issuer_membership_id);

-- Meta may independently verify the exact successful PG production before
-- queuing its basic identification. No User role or generic Grant permission.
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
 SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p
 ON p.permission_key='transfer.production_receipt.read'
 WHERE r.tenant_id IS NULL AND r.role_key='tenant.meta_runtime';
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
 WHERE f.revoked_at IS NULL AND EXISTS (SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
  WHERE a.principal_id=f.principal_id AND a.status='active' AND r.tenant_id IS NULL AND r.role_key='tenant.meta_runtime');

COMMIT;
