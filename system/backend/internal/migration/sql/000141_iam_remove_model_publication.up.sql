BEGIN;

CREATE TEMP TABLE publication_affected_principals ON COMMIT DROP AS
SELECT DISTINCT assignment.principal_id
FROM system.role_assignments assignment
JOIN system.role_permissions binding ON binding.role_id = assignment.role_id
JOIN system.permissions permission ON permission.id = binding.permission_id
WHERE assignment.status = 'active' AND (
 permission.permission_key LIKE 'model.materialization_group.%'
 OR permission.permission_key LIKE 'model.materialization_read.%'
 OR permission.permission_key LIKE 'model.task_provider.%'
 OR permission.permission_key LIKE 'quality.materialization_gate.%'
);

UPDATE system.principals principal
SET authorization_version = authorization_version + 1, updated_at = now()
FROM publication_affected_principals affected WHERE principal.id = affected.principal_id;

DELETE FROM system.role_permissions binding USING system.permissions permission
WHERE binding.permission_id = permission.id AND (
 permission.permission_key LIKE 'model.materialization_group.%'
 OR permission.permission_key LIKE 'model.materialization_read.%'
 OR permission.permission_key LIKE 'model.task_provider.%'
);
UPDATE system.permissions SET status = 'disabled', updated_at = now()
WHERE permission_key LIKE 'model.materialization_group.%'
 OR permission_key LIKE 'model.materialization_read.%'
 OR permission_key LIKE 'model.task_provider.%';

INSERT INTO system.permissions (permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
SELECT replace(permission_key,'quality.materialization_gate.','quality.data_validation.'),owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,
 replace(name_i18n_key,'quality.materialization_gate.','quality.data_validation.'),replace(description_i18n_key,'quality.materialization_gate.','quality.data_validation.'),'active'
FROM system.permissions WHERE permission_key LIKE 'quality.materialization_gate.%';

INSERT INTO system.role_permissions (role_id,permission_id,source_type,created_by_principal_id)
SELECT binding.role_id,new_permission.id,binding.source_type,binding.created_by_principal_id
FROM system.role_permissions binding
JOIN system.permissions old_permission ON old_permission.id=binding.permission_id
JOIN system.permissions new_permission ON new_permission.permission_key=replace(old_permission.permission_key,'quality.materialization_gate.','quality.data_validation.')
WHERE old_permission.permission_key LIKE 'quality.materialization_gate.%';
DELETE FROM system.role_permissions binding USING system.permissions permission
WHERE binding.permission_id=permission.id AND permission.permission_key LIKE 'quality.materialization_gate.%';
UPDATE system.permissions SET status='disabled',updated_at=now() WHERE permission_key LIKE 'quality.materialization_gate.%';

UPDATE system.refresh_token_families family
SET revoked_at = now(), revoked_reason = 'authorization_catalog_changed', updated_at = now()
FROM publication_affected_principals affected
WHERE family.principal_id = affected.principal_id AND family.revoked_at IS NULL;

COMMIT;
