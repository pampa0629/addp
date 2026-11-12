BEGIN;
ALTER TABLE system.engine_access_grants DROP CONSTRAINT source_grant_file_composition,
 ADD CONSTRAINT source_grant_file_composition CHECK ((CASE WHEN metadata_target IS NOT NULL THEN file_composition IS NULL
 WHEN system.valid_source_file_path(catalog_path,engine_id) OR system.valid_source_object_path(catalog_path,engine_id)
 THEN approval_mode IN ('independent','upload_creation','task_production') AND file_composition IS NOT NULL AND system.valid_source_file_composition(file_composition,catalog_path,engine_id)
 ELSE file_composition IS NULL END) IS TRUE);
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
 SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key='system.engine_access_initial_grant.execute'
 WHERE r.tenant_id IS NULL AND r.role_key='tenant.develop_runtime';
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
 WHERE f.revoked_at IS NULL AND EXISTS(SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
 WHERE a.principal_id=f.principal_id AND a.status='active' AND r.tenant_id IS NULL AND r.role_key='tenant.develop_runtime');
COMMIT;
