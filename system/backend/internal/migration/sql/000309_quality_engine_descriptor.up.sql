BEGIN;

DELETE FROM system.role_permissions
WHERE role_id = (SELECT id FROM system.roles WHERE tenant_id IS NULL AND role_key = 'tenant.quality_runtime')
  AND permission_id = (SELECT id FROM system.permissions WHERE permission_key = 'system.engine.read')
  AND source_type = 'product';

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT r.id, p.id, 'product'
FROM system.roles r JOIN system.permissions p ON p.permission_key = 'system.engine_descriptor.read'
WHERE r.tenant_id IS NULL AND r.role_key = 'tenant.quality_runtime';

COMMIT;
