BEGIN;

INSERT INTO system.permissions (permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status)
VALUES
    ('manager.export_provenance.read', 'manager', 'read', 'low', false, ARRAY['tenant']::text[], false,
     'permissions.manager.export_provenance.read.name', 'permissions.manager.export_provenance.read.description', 'active'),
    ('develop.export_provenance.read', 'develop', 'read', 'low', false, ARRAY['tenant']::text[], false,
     'permissions.develop.export_provenance.read.name', 'permissions.develop.export_provenance.read.description', 'active');

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission ON permission.permission_key IN
    ('manager.export_provenance.read', 'develop.export_provenance.read')
WHERE role.tenant_id IS NULL AND role.role_key = 'tenant.transfer_runtime';

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission ON permission.permission_key = 'system.runtime_registry.read'
WHERE role.tenant_id IS NULL AND role.role_key = 'platform.transfer_runtime'
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
