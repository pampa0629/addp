BEGIN;

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
)
VALUES (
    'tenant.model_runtime', 'roles.tenant.model_runtime.name',
    'roles.tenant.model_runtime.description', 'tenant_builtin',
    ARRAY['tenant']::text[], ARRAY['service_principal']::text[], true, 'active'
);

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'standard.dimension_hierarchy.read',
      'standard.domain.read',
      'standard.element.read',
      'standard.metric.read'
  )
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.model_runtime'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ORDER BY permission.permission_key;

INSERT INTO system.tenant_memberships (
    tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id
)
SELECT tenant.id, service_principal.id, 'active', 'bootstrap', tenant.initialized_at,
       tenant.initialized_by_principal_id
FROM system.tenants AS tenant
JOIN system.service_principals AS service_principal
  ON service_principal.name = 'addp-model'
WHERE tenant.initialized_at IS NOT NULL;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, tenant_id, status, valid_from,
    source_type, reason
)
SELECT service_principal.id, role.id, 'tenant', tenant.id, 'active',
       tenant.initialized_at, 'bootstrap', 'built-in service runtime'
FROM system.tenants AS tenant
JOIN system.service_principals AS service_principal
  ON service_principal.name = 'addp-model'
JOIN system.roles AS role
  ON role.tenant_id IS NULL
 AND role.role_key = 'tenant.model_runtime'
WHERE tenant.initialized_at IS NOT NULL;

COMMIT;
