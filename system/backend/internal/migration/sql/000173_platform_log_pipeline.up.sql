BEGIN;
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
SELECT 'monitor.'||resource||'.'||action,'monitor',action,risk,false,ARRAY['platform']::text[],false,
 'permissions.monitor.'||resource||'.'||action||'.name','permissions.monitor.'||resource||'.'||action||'.description','active'
FROM (VALUES ('log_pipeline','read','low'),('log_pipeline','update','medium'),('log_notification','read','low'),('log_notification','update','high'),('log_observation','create','medium')) seed(resource,action,risk);
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key IN ('monitor.log_pipeline.read','monitor.log_pipeline.update','monitor.log_notification.read','monitor.log_notification.update')
WHERE r.tenant_id IS NULL AND r.role_key='platform.system_administrator';
INSERT INTO system.roles(role_key,name_i18n_key,description_i18n_key,role_type,allowed_scope_types,allowed_principal_types,immutable,status)
VALUES ('platform.log_observer_runtime','roles.platform.log_observer_runtime.name','roles.platform.log_observer_runtime.description','platform_builtin',ARRAY['platform']::text[],ARRAY['service_principal']::text[],true,'active');
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key='monitor.log_observation.create'
WHERE r.tenant_id IS NULL AND r.role_key='platform.log_observer_runtime';
WITH principal AS (INSERT INTO system.principals(principal_type,status) VALUES ('service_principal','active') RETURNING id),
 service AS (INSERT INTO system.service_principals(id,name,description,owner_scope,created_by_principal_id)
 SELECT id,'addp-log-observer','ADDP Infra log pipeline observer','platform',id FROM principal RETURNING id)
INSERT INTO system.oauth_clients(client_id,display_name,client_type,client_secret_hash,service_principal_id,redirect_uris,grant_types,response_types,allowed_scopes,allowed_audiences,token_endpoint_auth_method,status)
SELECT 'addp-log-observer','ADDP Infra log pipeline observer','confidential',NULL,id,ARRAY[]::text[],ARRAY['client_credentials']::text[],ARRAY[]::text[],ARRAY['addp.api']::text[],ARRAY['addp.api']::text[],'client_secret_basic','disabled' FROM service;
INSERT INTO system.role_assignments(principal_id,role_id,scope_type,status,valid_from,source_type,grant_reason)
SELECT s.id,r.id,'platform','active',transaction_timestamp(),'bootstrap','Infra log observation only'
FROM system.service_principals s JOIN system.roles r ON r.role_key='platform.log_observer_runtime' AND r.tenant_id IS NULL WHERE s.name='addp-log-observer';
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES ('audit.event.create','system','create','high',false,ARRAY['platform']::text[],false,'permissions.audit.event.create.name','permissions.audit.event.create.description','active');
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key='audit.event.create' WHERE r.tenant_id IS NULL AND r.role_key='platform.monitor_runtime';
CREATE TEMP TABLE platform_log_affected ON COMMIT DROP AS SELECT DISTINCT a.principal_id FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id WHERE a.status='active' AND r.tenant_id IS NULL AND r.role_key IN ('platform.system_administrator','platform.monitor_runtime');
UPDATE system.principals p SET authorization_version=p.authorization_version+1,updated_at=now() FROM platform_log_affected a WHERE p.id=a.principal_id;
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now() FROM platform_log_affected a WHERE f.principal_id=a.principal_id AND f.revoked_at IS NULL;
COMMIT;
