BEGIN;

-- Catalog definition only. No built-in Role, Role Permission, Assignment,
-- resource Grant, authorization version or existing token is modified.
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,
 delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES ('develop.file_execution.read','develop','read','low',false,ARRAY['tenant']::text[],false,
 'permissions.develop.file_execution.read.name',
 'permissions.develop.file_execution.read.description','active');

COMMIT;
