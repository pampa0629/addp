BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES (
    'standard.glossary.publish', 'standard', 'publish', 'high', false,
    ARRAY['tenant','department','project_group']::text[], true,
    'permissions.standard.glossary.publish.name',
    'permissions.standard.glossary.publish.description', 'active'
)
ON CONFLICT (permission_key) DO UPDATE SET
    action = EXCLUDED.action,
    risk_level = EXCLUDED.risk_level,
    allowed_scope_types = EXCLUDED.allowed_scope_types,
    tenant_customizable = EXCLUDED.tenant_customizable,
    name_i18n_key = EXCLUDED.name_i18n_key,
    description_i18n_key = EXCLUDED.description_i18n_key,
    status = 'active',
    updated_at = transaction_timestamp();

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'standard.glossary.publish'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.governance_manager'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

DELETE FROM system.role_permissions AS role_permission
USING system.permissions AS permission
WHERE role_permission.permission_id = permission.id
  AND permission.permission_key IN ('standard.glossary.approve', 'standard.glossary.offline');

UPDATE system.permissions
SET status = 'disabled',
    updated_at = transaction_timestamp()
WHERE permission_key IN ('standard.glossary.approve', 'standard.glossary.offline')
  AND status <> 'disabled';

COMMIT;
