BEGIN;

INSERT INTO system.permissions (permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status)
VALUES ('meta.authorization_facts.read', 'meta', 'read', 'low', false,
    ARRAY['tenant']::text[], false,
    'permissions.meta.authorization_facts.read.name',
    'permissions.meta.authorization_facts.read.description', 'active');

-- Existing Role Permission triggers advance only affected principals' versions.
INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT r.id, p.id, 'product'
FROM system.roles r JOIN system.permissions p ON p.permission_key = 'meta.authorization_facts.read'
WHERE r.tenant_id IS NULL AND r.role_key = 'tenant.system_runtime';

CREATE TEMP TABLE authorization_facts_affected ON COMMIT DROP AS
SELECT DISTINCT a.principal_id FROM system.role_assignments a
JOIN system.roles r ON r.id=a.role_id
WHERE a.status='active' AND r.tenant_id IS NULL AND r.role_key='tenant.system_runtime';
UPDATE system.refresh_token_families f
SET revoked_at=now(), revoked_reason='authorization_catalog_changed', updated_at=now()
FROM authorization_facts_affected a
WHERE f.principal_id=a.principal_id AND f.revoked_at IS NULL;

COMMIT;
