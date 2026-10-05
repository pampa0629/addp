BEGIN;
INSERT INTO system.permissions (permission_key,owner_module,action,risk_level,delegable,
    allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES ('system.observability_identity.read','system','read','low',false,
    ARRAY['platform']::text[],false,'permissions.system.observability_identity.read.name',
    'permissions.system.observability_identity.read.description','active');
INSERT INTO system.role_permissions (role_id,permission_id,source_type)
SELECT role.id,permission.id,'product' FROM system.roles role JOIN system.permissions permission
ON permission.permission_key = 'system.observability_identity.read'
WHERE role.role_key = 'platform.monitor_runtime' AND role.tenant_id IS NULL AND role.status = 'active'
ON CONFLICT DO NOTHING;
CREATE TEMP TABLE observability_identity_affected_principals ON COMMIT DROP AS
SELECT DISTINCT assignment.principal_id FROM system.role_assignments assignment
JOIN system.roles role ON role.id = assignment.role_id
WHERE assignment.status = 'active' AND role.role_key = 'platform.monitor_runtime' AND role.tenant_id IS NULL;
UPDATE system.principals principal SET authorization_version = principal.authorization_version + 1, updated_at = now()
FROM observability_identity_affected_principals affected WHERE principal.id = affected.principal_id;
UPDATE system.refresh_token_families family SET revoked_at = now(), revoked_reason = 'authorization_catalog_changed', updated_at = now()
FROM observability_identity_affected_principals affected WHERE family.principal_id = affected.principal_id AND family.revoked_at IS NULL;
COMMIT;
