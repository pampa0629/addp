BEGIN;
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES ('monitor.resource_observation.read','monitor','read','low',false,ARRAY['platform']::text[],false,'permissions.monitor.resource_observation.read.name','permissions.monitor.resource_observation.read.description','active');
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key='monitor.resource_observation.read'
WHERE r.tenant_id IS NULL AND r.role_key='platform.system_administrator';
CREATE TEMP TABLE resource_observation_affected ON COMMIT DROP AS SELECT DISTINCT a.principal_id FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id WHERE a.status='active' AND r.tenant_id IS NULL AND r.role_key='platform.system_administrator';
-- The role_permissions trigger advances affected authorization versions once.
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now() FROM resource_observation_affected a WHERE f.principal_id=a.principal_id AND f.revoked_at IS NULL;
COMMIT;
