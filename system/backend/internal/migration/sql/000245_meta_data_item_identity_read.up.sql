BEGIN;

-- Minimal owner identity enumeration for System's effective-permission list.
-- No human role, Catalog runtime, data Grant, Deny, or historical binding changes.
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,
 tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES ('meta.data_item_identity.read','meta','read','low',false,ARRAY['tenant']::text[],false,
 'permissions.meta.data_item_identity.read.name','permissions.meta.data_item_identity.read.description','active');
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
 SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key='meta.data_item_identity.read'
 WHERE r.tenant_id IS NULL AND r.role_key='tenant.system_runtime';
UPDATE system.refresh_token_families f SET revoked_at=statement_timestamp(),revoked_reason='authorization_catalog_changed',updated_at=statement_timestamp()
 WHERE f.revoked_at IS NULL AND EXISTS (SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
  WHERE a.principal_id=f.principal_id AND a.status='active' AND r.tenant_id IS NULL AND r.role_key='tenant.system_runtime');

COMMIT;
