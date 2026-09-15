BEGIN;
UPDATE system.permissions SET status='active', tenant_customizable=false, updated_at=now()
WHERE permission_key IN ('model.task_provider.execute','model.task_provider.read');
INSERT INTO system.role_permissions(role_id,permission_id,source_type,created_by_principal_id)
SELECT r.id,p.id,'product',NULL FROM system.roles r JOIN system.permissions p
ON p.permission_key IN ('model.task_provider.execute','model.task_provider.read')
WHERE r.role_key='tenant.orchestrator_runtime' AND r.tenant_id IS NULL AND r.status='active'
ON CONFLICT(role_id,permission_id) DO NOTHING;
UPDATE system.principals SET authorization_version=authorization_version+1,updated_at=now()
WHERE id IN (SELECT a.principal_id FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id WHERE r.role_key='tenant.orchestrator_runtime' AND a.status='active' UNION SELECT id FROM system.service_principals WHERE name='addp-orchestrator');
COMMIT;
