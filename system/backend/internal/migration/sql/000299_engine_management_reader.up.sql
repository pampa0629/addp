BEGIN;

-- Publish the approved Tenant User template only; never create assignments.
INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
) VALUES (
    'tenant.engine_management_reader',
    'roles.tenant.engine_management_reader.name',
    'roles.tenant.engine_management_reader.description',
    'tenant_builtin', ARRAY['tenant']::text[], ARRAY['user']::text[], true, 'active'
);

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission ON permission.permission_key = 'system.engine.read'
WHERE role.tenant_id IS NULL AND role.role_key = 'tenant.engine_management_reader'
ORDER BY permission.permission_key;

COMMIT;
