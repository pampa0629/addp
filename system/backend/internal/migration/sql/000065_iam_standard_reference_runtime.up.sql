BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES (
    'model.standard_reference.update', 'model', 'update', 'high', false,
    ARRAY['tenant']::text[], false,
    'permissions.model.standard_reference.update.name',
    'permissions.model.standard_reference.update.description', 'active'
)
ON CONFLICT (permission_key) DO NOTHING;

INSERT INTO system.roles (
    tenant_id, role_key, name, description, name_i18n_key,
    description_i18n_key, role_type, allowed_scope_types,
    allowed_principal_types, immutable, status, created_by_principal_id
) VALUES (
    NULL, 'tenant.standard_runtime', NULL, NULL,
    'roles.tenant.standard_runtime.name',
    'roles.tenant.standard_runtime.description', 'tenant_builtin',
    ARRAY['tenant']::text[], ARRAY['service_principal']::text[],
    true, 'active', NULL
)
ON CONFLICT ON CONSTRAINT roles_tenant_id_role_key_key DO NOTHING;

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'model.standard_reference.update'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.standard_runtime'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO system.tenant_memberships (
    tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id
)
SELECT tenant.id, service_principal.id, 'active', 'bootstrap', tenant.initialized_at,
       tenant.initialized_by_principal_id
FROM system.tenants AS tenant
JOIN system.service_principals AS service_principal
  ON service_principal.name = 'addp-standard'
WHERE tenant.initialized_at IS NOT NULL
ON CONFLICT (tenant_id, principal_id) DO NOTHING;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, tenant_id, status, valid_from,
    source_type, reason
)
SELECT service_principal.id, role.id, 'tenant', tenant.id, 'active',
       tenant.initialized_at, 'bootstrap', 'built-in service runtime'
FROM system.tenants AS tenant
JOIN system.service_principals AS service_principal
  ON service_principal.name = 'addp-standard'
JOIN system.roles AS role
  ON role.tenant_id IS NULL
 AND role.role_key = 'tenant.standard_runtime'
WHERE tenant.initialized_at IS NOT NULL
ON CONFLICT (
    principal_id, role_id, scope_type, tenant_id, department_id, project_group_id
)
WHERE status = 'active'
DO NOTHING;

COMMIT;
