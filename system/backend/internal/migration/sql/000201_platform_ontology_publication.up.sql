BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status
) VALUES (
    'ontology.platform_definition.publish', 'ontology', 'publish', 'high', false,
    ARRAY['platform']::text[], false,
    'permissions.ontology.platform_definition.publish.name',
    'permissions.ontology.platform_definition.publish.description', 'active'
);

-- Only the platform machine role may publish deployed definitions. No user or
-- Tenant role is expanded and no new assignment or runtime identity is made.
INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT r.id, p.id, 'product'
FROM system.roles r JOIN system.permissions p
    ON p.permission_key = 'ontology.platform_definition.publish'
WHERE r.tenant_id IS NULL AND r.role_key = 'platform.ontology_runtime';

-- Existing version triggers invalidate previously issued service tokens.
UPDATE system.refresh_token_families f
SET revoked_at = statement_timestamp(), revoked_reason = 'authorization_catalog_changed', updated_at = statement_timestamp()
WHERE f.revoked_at IS NULL AND EXISTS (
    SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id = a.role_id
    WHERE a.principal_id = f.principal_id AND a.status = 'active'
      AND a.scope_type = 'platform' AND r.tenant_id IS NULL AND r.role_key = 'platform.ontology_runtime'
);

COMMIT;
