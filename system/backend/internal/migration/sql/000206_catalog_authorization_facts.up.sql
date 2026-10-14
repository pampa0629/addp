BEGIN;

-- Reuse Meta's one facts contract; no human assignment or source Grant.
INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT r.id, p.id, 'product'
FROM system.roles r JOIN system.permissions p ON p.permission_key = 'meta.authorization_facts.read'
WHERE r.tenant_id IS NULL AND r.role_key = 'tenant.catalog_runtime';

-- Role Permission triggers advance the affected service authority exactly once.
CREATE TEMP TABLE catalog_authorization_facts_affected ON COMMIT DROP AS
SELECT DISTINCT a.principal_id FROM system.role_assignments a
JOIN system.roles r ON r.id = a.role_id
WHERE a.status = 'active' AND r.tenant_id IS NULL AND r.role_key = 'tenant.catalog_runtime';
UPDATE system.refresh_token_families f
SET revoked_at = now(), revoked_reason = 'authorization_catalog_changed', updated_at = now()
FROM catalog_authorization_facts_affected a
WHERE f.principal_id = a.principal_id AND f.revoked_at IS NULL;

COMMIT;
