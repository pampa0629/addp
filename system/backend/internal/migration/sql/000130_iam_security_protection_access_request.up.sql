BEGIN;

DELETE FROM system.role_permissions
WHERE permission_id IN (
    SELECT id FROM system.permissions
    WHERE permission_key IN (
        'security.protection_exemption.create',
        'security.protection_exemption.update'
    )
);

UPDATE system.permissions
SET status = 'disabled', updated_at = transaction_timestamp()
WHERE permission_key IN (
    'security.protection_exemption.create',
    'security.protection_exemption.update'
);

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES
    (
        'security.protection_access_request.create', 'security', 'create', 'high', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.protection_access_request.create.name',
        'permissions.security.protection_access_request.create.description', 'active'
    ),
    (
        'security.protection_access_request.read', 'security', 'read', 'high', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.protection_access_request.read.name',
        'permissions.security.protection_access_request.read.description', 'active'
    ),
    (
        'security.protection_access_request.update', 'security', 'update', 'critical', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.protection_access_request.update.name',
        'permissions.security.protection_access_request.update.description', 'active'
    );

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'security.protection_access_request.create',
      'security.protection_access_request.read',
      'security.protection_access_request.update'
  )
WHERE role.tenant_id IS NULL
  AND (
      role.role_key IN ('tenant.administrator', 'tenant.governance_manager')
      OR (
          role.role_key IN ('tenant.data_engineer', 'tenant.data_steward', 'tenant.data_viewer')
          AND permission.permission_key IN (
              'security.protection_access_request.create',
              'security.protection_access_request.read'
          )
      )
  )
ORDER BY role.role_key, permission.permission_key;

COMMIT;
