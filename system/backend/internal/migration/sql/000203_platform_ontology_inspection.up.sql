BEGIN;

INSERT INTO system.permissions (permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status)
VALUES ('ontology.platform_definition.read', 'ontology', 'read', 'low', false,
    ARRAY['platform']::text[], false, 'permissions.ontology.platform_definition.read.name',
    'permissions.ontology.platform_definition.read.description', 'active');

-- Read-only user supervision is separate from deployment publication and Tenant semantics.
INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT r.id, p.id, 'product' FROM system.roles r JOIN system.permissions p
    ON p.permission_key = 'ontology.platform_definition.read'
WHERE r.tenant_id IS NULL AND r.role_key = 'platform.system_administrator';

-- Role-permission triggers advance authorization_version; close affected old families.
UPDATE system.refresh_token_families f
SET revoked_at = statement_timestamp(), revoked_reason = 'authorization_catalog_changed', updated_at = statement_timestamp()
WHERE f.revoked_at IS NULL AND EXISTS (
    SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id = a.role_id
    WHERE a.principal_id = f.principal_id AND a.status = 'active'
      AND a.scope_type = 'platform' AND r.tenant_id IS NULL AND r.role_key = 'platform.system_administrator'
);
COMMIT;
