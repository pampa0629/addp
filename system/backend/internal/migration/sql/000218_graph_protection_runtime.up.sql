BEGIN;

-- Only the fixed Graph service receives projection feed/ack and tenant discovery.
INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role JOIN system.permissions permission
 ON (role.role_key = 'tenant.graph_runtime' AND permission.permission_key IN ('security.protection_projection.read', 'security.protection_projection.update'))
 OR (role.role_key = 'platform.graph_runtime' AND permission.permission_key = 'platform.tenant.read')
WHERE role.tenant_id IS NULL AND role.status = 'active' AND permission.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
