BEGIN;

-- The explicit authorization administrator may also handle ordinary reads.
-- This changes the assigned role, not engine delegations or content Grants.
INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT r.id, p.id, 'product'
FROM system.roles r JOIN system.permissions p ON p.permission_key IN (
    'iam.department.read', 'iam.project_group.read', 'system.engine_catalog.read',
    'system.engine_access_approval_requirement.initialize', 'system.engine_access_approval_requirement.read',
    'system.engine_access_grant.create', 'system.engine_access_grant.read', 'system.engine_access_grant.revoke'
)
WHERE r.tenant_id IS NULL AND r.role_key = 'tenant.engine_access_delegation_administrator';

CREATE TEMP TABLE engine_authorization_administrator_affected ON COMMIT DROP AS
SELECT DISTINCT a.principal_id FROM system.role_assignments a JOIN system.roles r ON r.id = a.role_id
WHERE a.status = 'active' AND r.tenant_id IS NULL AND r.role_key = 'tenant.engine_access_delegation_administrator';
-- Existing role-permission triggers advance authorization versions.
UPDATE system.refresh_token_families f SET revoked_at = now(), revoked_reason = 'authorization_catalog_changed', updated_at = now()
FROM engine_authorization_administrator_affected a
WHERE f.principal_id = a.principal_id AND f.revoked_at IS NULL;

COMMIT;
