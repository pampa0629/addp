BEGIN;

-- Recovery only: no acceptance Permission, User role or resource Grant.
INSERT INTO system.permissions (permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status)
VALUES ('system.engine_access_fulfillment.execute', 'system', 'execute', 'high', false,
    ARRAY['tenant']::text[], false,
    'permissions.system.engine_access_fulfillment.execute.name',
    'permissions.system.engine_access_fulfillment.execute.description', 'active');

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT r.id, p.id, 'product' FROM system.roles r JOIN system.permissions p
    ON p.permission_key = 'system.engine_access_fulfillment.execute'
WHERE r.tenant_id IS NULL AND r.role_key = 'tenant.catalog_runtime';

CREATE TEMP TABLE fulfillment_recovery_affected_principals ON COMMIT DROP AS
SELECT DISTINCT a.principal_id FROM system.role_assignments a JOIN system.roles r ON r.id = a.role_id
WHERE a.status = 'active' AND r.tenant_id IS NULL AND r.role_key = 'tenant.catalog_runtime';

-- The role_permissions INSERT already advances affected authorization versions
-- through the IAM trigger. Do not advance the same change a second time.
UPDATE system.refresh_token_families f SET revoked_at = now(), revoked_reason = 'authorization_catalog_changed', updated_at = now()
FROM fulfillment_recovery_affected_principals a WHERE f.principal_id = a.principal_id AND f.revoked_at IS NULL;

COMMIT;
