BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES (
    'system.engine_catalog.read', 'system', 'read', 'low', false,
    ARRAY['tenant', 'department', 'project_group']::text[], true,
    'permissions.system.engine_catalog.read.name',
    'permissions.system.engine_catalog.read.description', 'active'
);

-- Catalog browsing is a dependency of these user workflows. Existing
-- builtin engine readers keep access to the narrower catalog endpoints.
INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, catalog_permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS catalog_permission
  ON catalog_permission.permission_key = 'system.engine_catalog.read'
 AND catalog_permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
  AND (
    role.role_key IN (
      'tenant.data_steward',
      'tenant.governance_manager',
      'tenant.graph_engineer',
      'tenant.service_publisher'
    )
    OR EXISTS (
      SELECT 1
      FROM system.role_permissions AS existing_grant
      JOIN system.permissions AS existing_permission
        ON existing_permission.id = existing_grant.permission_id
      WHERE existing_grant.role_id = role.id
        AND existing_permission.permission_key = 'system.engine.read'
    )
  )
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- Quality plan target selection reads Meta's resource tree.
INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'meta.catalog.read'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.governance_manager'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

CREATE TEMP TABLE engine_catalog_affected_principals ON COMMIT DROP AS
SELECT DISTINCT assignment.principal_id
FROM system.role_assignments AS assignment
JOIN system.roles AS role ON role.id = assignment.role_id
JOIN system.role_permissions AS catalog_grant ON catalog_grant.role_id = role.id
JOIN system.permissions AS permission ON permission.id = catalog_grant.permission_id
WHERE assignment.status = 'active'
  AND permission.permission_key IN ('system.engine_catalog.read', 'meta.catalog.read')
  AND (permission.permission_key = 'system.engine_catalog.read'
    OR role.role_key = 'tenant.governance_manager');

UPDATE system.principals AS principal
SET authorization_version = principal.authorization_version + 1,
    updated_at = now()
FROM engine_catalog_affected_principals AS affected
WHERE principal.id = affected.principal_id;

UPDATE system.refresh_token_families AS family
SET revoked_at = now(),
    revoked_reason = 'authorization_catalog_changed',
    updated_at = now()
FROM engine_catalog_affected_principals AS affected
WHERE family.principal_id = affected.principal_id
  AND family.revoked_at IS NULL;

COMMIT;
