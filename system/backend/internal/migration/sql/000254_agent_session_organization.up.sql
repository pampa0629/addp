BEGIN;

-- Personal session organization only; no business Tool scopes or data grants.
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,
 tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES ('agent.session.update','agent','update','medium',false,ARRAY['tenant']::text[],true,
 'permissions.agent.session.update.name','permissions.agent.session.update.description','active');
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
 SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key='agent.session.update'
 WHERE r.tenant_id IS NULL AND r.role_key='tenant.ai_user' AND r.role_type='tenant_builtin';

UPDATE system.refresh_token_families f SET revoked_at=statement_timestamp(),revoked_reason='authorization_catalog_changed',updated_at=statement_timestamp()
 WHERE f.revoked_at IS NULL AND EXISTS (SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
  WHERE a.principal_id=f.principal_id AND a.status='active' AND r.tenant_id IS NULL AND r.role_key='tenant.ai_user');

COMMIT;
