BEGIN;

INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES
 ('monitor.trace.read','monitor','read','low',false,ARRAY['platform']::text[],false,'permissions.monitor.trace.read.name','permissions.monitor.trace.read.description','active');

INSERT INTO system.role_permissions(role_id,permission_id,source_type)
 SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key IN ('monitor.trace.read')
 WHERE r.tenant_id IS NULL AND r.role_type='platform_builtin' AND r.role_key='platform.system_administrator';

-- Existing Role/Permission triggers advance authorization versions. Revoke old refresh families.
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
 WHERE f.revoked_at IS NULL AND EXISTS (SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
 WHERE a.principal_id=f.principal_id AND a.status='active' AND r.tenant_id IS NULL AND r.role_key='platform.system_administrator');

COMMIT;
