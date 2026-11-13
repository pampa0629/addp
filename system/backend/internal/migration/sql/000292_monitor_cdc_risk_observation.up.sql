BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status
) VALUES (
    'transfer.cdc_risk_observation.read', 'transfer', 'read', 'low', false,
    ARRAY['platform']::text[], false,
    'permissions.transfer.cdc_risk_observation.read.name',
    'permissions.transfer.cdc_risk_observation.read.description', 'active'
);

-- Only the existing Monitor Platform Service role receives the safe inventory.
-- No User/Tenant role, Assignment, OAuth Client or data Grant is introduced.
INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT r.id, p.id, 'product'
FROM system.roles r JOIN system.permissions p
    ON p.permission_key = 'transfer.cdc_risk_observation.read'
WHERE r.tenant_id IS NULL AND r.role_key = 'platform.monitor_runtime';

-- Role Permission triggers advance affected machine authorization versions.
UPDATE system.refresh_token_families f
SET revoked_at = statement_timestamp(), revoked_reason = 'authorization_catalog_changed', updated_at = statement_timestamp()
WHERE f.revoked_at IS NULL AND f.context_type = 'platform' AND EXISTS (
    SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id = a.role_id
    WHERE a.principal_id = f.principal_id AND a.status = 'active'
      AND a.scope_type = 'platform' AND r.tenant_id IS NULL AND r.role_key = 'platform.monitor_runtime'
);

COMMIT;
