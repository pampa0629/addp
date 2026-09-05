BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES
    ('copilot.standard_document.execute', 'copilot', 'execute', 'medium', false, ARRAY['tenant']::text[], false, 'permissions.copilot.standard_document.execute.name', 'permissions.copilot.standard_document.execute.description', 'active'),
    ('standard.document.publish', 'standard', 'publish', 'high', false, ARRAY['tenant','department','project_group']::text[], true, 'permissions.standard.document.publish.name', 'permissions.standard.document.publish.description', 'active'),
    ('standard.document_extraction.create', 'standard', 'create', 'medium', false, ARRAY['tenant','department','project_group']::text[], true, 'permissions.standard.document_extraction.create.name', 'permissions.standard.document_extraction.create.description', 'active')
ON CONFLICT (permission_key) DO UPDATE SET
    owner_module = EXCLUDED.owner_module,
    action = EXCLUDED.action,
    risk_level = EXCLUDED.risk_level,
    delegable = EXCLUDED.delegable,
    allowed_scope_types = EXCLUDED.allowed_scope_types,
    tenant_customizable = EXCLUDED.tenant_customizable,
    name_i18n_key = EXCLUDED.name_i18n_key,
    description_i18n_key = EXCLUDED.description_i18n_key,
    status = EXCLUDED.status,
    updated_at = transaction_timestamp();

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'standard.document.publish',
      'standard.document_extraction.create'
  )
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.governance_manager'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'copilot.standard_document.execute'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.standard_runtime'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
