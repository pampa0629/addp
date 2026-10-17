BEGIN;
ALTER TABLE system.task_authorization_subjects
 DROP CONSTRAINT task_authorization_subjects_owner_module_check,
 DROP CONSTRAINT task_authorization_subjects_task_type_check,
 ADD CONSTRAINT task_authorization_subjects_owner_type_check CHECK (
  (owner_module='orchestrator' AND task_type='orchestration') OR
  (owner_module='transfer' AND task_type='sync')
 );

INSERT INTO system.role_permissions (role_id,permission_id,source_type)
SELECT r.id,p.id,'product'
FROM system.roles r CROSS JOIN system.permissions p
WHERE r.tenant_id IS NULL AND r.role_key='tenant.transfer_runtime'
 AND p.permission_key='system.task_authorization.execute'
ON CONFLICT DO NOTHING;
COMMIT;
