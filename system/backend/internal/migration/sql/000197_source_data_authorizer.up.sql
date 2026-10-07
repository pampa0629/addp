BEGIN;

-- Publish a capability template only. Assignments and engine delegations remain explicit.
INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
) VALUES (
    'tenant.source_data_authorizer',
    'roles.tenant.source_data_authorizer.name',
    'roles.tenant.source_data_authorizer.description',
    'tenant_builtin', ARRAY['tenant']::text[], ARRAY['user']::text[], true, 'active'
);

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission ON permission.permission_key IN (
    'iam.department.read',
    'iam.project_group.read',
    'iam.tenant_membership.read',
    'system.engine.read',
    'system.engine_access_approval_requirement.initialize',
    'system.engine_access_approval_requirement.read',
    'system.engine_access_grant.create',
    'system.engine_access_grant.read',
    'system.engine_access_grant.revoke',
    'system.engine_catalog.read'
)
WHERE role.tenant_id IS NULL AND role.role_key = 'tenant.source_data_authorizer'
ORDER BY permission.permission_key;

COMMIT;
