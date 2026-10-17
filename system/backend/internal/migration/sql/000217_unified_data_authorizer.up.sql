BEGIN;

-- One explicitly assigned human role handles both approval workflows.
-- Approval mode, business confirmation, and engine scope remain independent checks.
INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT r.id, p.id, 'product'
FROM system.roles r JOIN system.permissions p ON p.permission_key IN (
    'catalog.entry.read', 'system.engine_access_fulfillment.create'
)
WHERE r.tenant_id IS NULL AND r.role_key = 'tenant.source_data_authorizer';

-- Role-permission triggers advance affected principals' authorization versions.
UPDATE system.refresh_token_families f
SET revoked_at = now(), revoked_reason = 'authorization_catalog_changed', updated_at = now()
WHERE f.revoked_at IS NULL AND EXISTS (
    SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id = a.role_id
    WHERE a.principal_id = f.principal_id AND a.status = 'active'
      AND r.tenant_id IS NULL AND r.role_key = 'tenant.source_data_authorizer'
);

COMMIT;
