BEGIN;

INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES
 ('monitor.http_observation.read','monitor','read','low',false,ARRAY['platform']::text[],false,'permissions.monitor.http_observation.read.name','permissions.monitor.http_observation.read.description','active'),
 ('monitor.platform_http_rule.create','monitor','create','medium',false,ARRAY['platform']::text[],false,'permissions.monitor.platform_http_rule.create.name','permissions.monitor.platform_http_rule.create.description','active'),
 ('monitor.platform_http_rule.delete','monitor','delete','high',false,ARRAY['platform']::text[],false,'permissions.monitor.platform_http_rule.delete.name','permissions.monitor.platform_http_rule.delete.description','active'),
 ('monitor.platform_http_rule.read','monitor','read','low',false,ARRAY['platform']::text[],false,'permissions.monitor.platform_http_rule.read.name','permissions.monitor.platform_http_rule.read.description','active'),
 ('monitor.platform_http_rule.update','monitor','update','medium',false,ARRAY['platform']::text[],false,'permissions.monitor.platform_http_rule.update.name','permissions.monitor.platform_http_rule.update.description','active');

INSERT INTO system.role_permissions(role_id,permission_id,source_type)
 SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key IN ('monitor.http_observation.read','monitor.platform_http_rule.create','monitor.platform_http_rule.delete','monitor.platform_http_rule.read','monitor.platform_http_rule.update')
 WHERE r.tenant_id IS NULL AND r.role_type='platform_builtin' AND r.role_key='platform.system_administrator';

-- Existing Role/Permission triggers advance authorization versions. Revoke old refresh families.
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
 WHERE f.revoked_at IS NULL AND EXISTS (SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
 WHERE a.principal_id=f.principal_id AND a.status='active' AND r.tenant_id IS NULL AND r.role_key='platform.system_administrator');

COMMIT;
