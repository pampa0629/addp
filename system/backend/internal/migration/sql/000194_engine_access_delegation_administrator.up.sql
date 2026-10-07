BEGIN;

-- Publish a dedicated template only; existing roles and accounts keep their grants.
INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
) VALUES (
    'tenant.engine_access_delegation_administrator',
    'roles.tenant.engine_access_delegation_administrator.name',
    'roles.tenant.engine_access_delegation_administrator.description',
    'tenant_builtin', ARRAY['tenant']::text[], ARRAY['user']::text[], true, 'active'
);

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission ON permission.permission_key IN (
    'iam.tenant_membership.read',
    'system.engine.read',
    'system.engine_access_delegation.create',
    'system.engine_access_delegation.read',
    'system.engine_access_delegation.revoke'
)
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.engine_access_delegation_administrator'
ORDER BY permission.permission_key;

COMMIT;
