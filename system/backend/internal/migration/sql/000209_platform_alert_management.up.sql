BEGIN;
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
SELECT 'monitor.'||resource||'.'||action,'monitor',action,risk,false,ARRAY['platform']::text[],false,
 'permissions.monitor.'||resource||'.'||action||'.name','permissions.monitor.'||resource||'.'||action||'.description','active'
FROM (VALUES ('platform_alert','read','low'),('platform_alert','update','medium'),('platform_alert_notification','read','low'),('platform_alert_notification','update','high')) seed(resource,action,risk);
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key IN
 ('monitor.platform_alert.read','monitor.platform_alert.update','monitor.platform_alert_notification.read','monitor.platform_alert_notification.update')
WHERE r.tenant_id IS NULL AND r.role_key='platform.system_administrator';
DELETE FROM system.role_permissions rp USING system.roles r,system.permissions p
WHERE rp.role_id=r.id AND rp.permission_id=p.id AND r.tenant_id IS NULL AND r.role_key='platform.system_administrator'
 AND p.permission_key IN ('monitor.log_notification.read','monitor.log_notification.update');
UPDATE system.permissions SET status='disabled'
WHERE permission_key IN ('monitor.log_notification.read','monitor.log_notification.update');
-- Existing role-permission triggers advance authorization_version. Revoke the
-- affected principal's old refresh families using the same publication policy.
UPDATE system.refresh_token_families f
SET revoked_at=statement_timestamp(),revoked_reason='authorization_catalog_changed',updated_at=statement_timestamp()
WHERE f.revoked_at IS NULL AND EXISTS (
 SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
 WHERE a.principal_id=f.principal_id AND a.status='active' AND a.scope_type='platform'
 AND r.tenant_id IS NULL AND r.role_key='platform.system_administrator');
COMMIT;
