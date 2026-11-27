BEGIN;

-- Transport qualification only. Current human source Grants remain independent.
INSERT INTO system.permissions(permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status)
VALUES ('system.transfer_field_connection.read', 'system', 'read', 'high', false,
    ARRAY['tenant']::text[], false, 'permissions.system.transfer_field_connection.read.name',
    'permissions.system.transfer_field_connection.read.description', 'active');
INSERT INTO system.role_permissions(role_id, permission_id, source_type)
SELECT r.id, p.id, 'product' FROM system.roles r JOIN system.permissions p
ON p.permission_key = 'system.transfer_field_connection.read'
WHERE r.tenant_id IS NULL AND r.role_key = 'tenant.transfer_runtime';
UPDATE system.refresh_token_families f
SET revoked_at=now(), revoked_reason='authorization_catalog_changed', updated_at=now()
WHERE f.revoked_at IS NULL AND EXISTS (
    SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
    WHERE a.principal_id=f.principal_id AND a.status='active'
      AND r.tenant_id IS NULL AND r.role_key='tenant.transfer_runtime'
);

COMMIT;
