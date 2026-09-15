BEGIN;
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES ('model.metric_implementation.create','model','create','medium',false,ARRAY['tenant']::text[],true,'permissions.model.metric_implementation.create.name','permissions.model.metric_implementation.create.description','active'),
('model.metric_implementation.delete','model','delete','high',false,ARRAY['tenant']::text[],true,'permissions.model.metric_implementation.delete.name','permissions.model.metric_implementation.delete.description','active'),
('model.metric_implementation.offline','model','offline','high',false,ARRAY['tenant']::text[],true,'permissions.model.metric_implementation.offline.name','permissions.model.metric_implementation.offline.description','active'),
('model.metric_implementation.publish','model','publish','high',false,ARRAY['tenant']::text[],true,'permissions.model.metric_implementation.publish.name','permissions.model.metric_implementation.publish.description','active'),
('model.metric_implementation.read','model','read','low',false,ARRAY['tenant']::text[],true,'permissions.model.metric_implementation.read.name','permissions.model.metric_implementation.read.description','active'),
('model.metric_implementation.update','model','update','medium',false,ARRAY['tenant']::text[],true,'permissions.model.metric_implementation.update.name','permissions.model.metric_implementation.update.description','active')
ON CONFLICT(permission_key) DO UPDATE SET status='active',updated_at=now();
INSERT INTO system.role_permissions(role_id,permission_id,source_type,created_by_principal_id)
SELECT r.id,p.id,'product',NULL FROM system.roles r JOIN system.permissions p ON
 (r.role_key='tenant.data_architect' AND p.permission_key LIKE 'model.metric_implementation.%') OR
 (r.role_key='tenant.service_runtime' AND p.permission_key='model.metric_implementation.read')
WHERE r.tenant_id IS NULL AND r.status='active'
ON CONFLICT(role_id,permission_id) DO NOTHING;
UPDATE system.principals SET authorization_version=authorization_version+1,updated_at=now()
WHERE id IN (SELECT a.principal_id FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id WHERE r.role_key IN ('tenant.data_architect','tenant.service_runtime') AND a.status='active' UNION SELECT id FROM system.service_principals WHERE name='addp-service');
COMMIT;
