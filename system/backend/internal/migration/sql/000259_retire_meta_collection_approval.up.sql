BEGIN;
-- Retire per-task approval. No human role gains scan permissions.
DROP TABLE system.meta_collection_qualifications;
DROP FUNCTION system.guard_meta_collection_qualification_history();
CREATE TEMP TABLE retired_meta_collection_principals ON COMMIT DROP AS
 SELECT DISTINCT a.principal_id FROM system.role_assignments a
 JOIN system.role_permissions rp ON rp.role_id=a.role_id
 JOIN system.permissions p ON p.id=rp.permission_id
 WHERE a.status='active' AND p.permission_key LIKE 'system.meta_collection_qualification.%';
DELETE FROM system.role_permissions rp USING system.permissions p WHERE rp.permission_id=p.id
 AND p.permission_key LIKE 'system.meta_collection_qualification.%';
UPDATE system.permissions SET status='disabled',updated_at=now()
 WHERE permission_key LIKE 'system.meta_collection_qualification.%';
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
 VALUES ('system.meta_collection.execute','system','execute','high',false,ARRAY['tenant']::text[],false,'permissions.system.meta_collection.execute.name','permissions.system.meta_collection.execute.description','active');
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
 SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key='system.meta_collection.execute'
 WHERE r.role_key='tenant.meta_runtime' AND r.tenant_id IS NULL;
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
 FROM retired_meta_collection_principals a WHERE f.principal_id=a.principal_id AND f.revoked_at IS NULL;
COMMIT;
