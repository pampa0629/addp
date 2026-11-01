-- Independent manual cancellation; administrators assign it explicitly.
-- No role or account receives this permission through this migration.
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES ('meta.scan_task.cancel','meta','cancel','medium',false,ARRAY['tenant']::text[],true,'permissions.meta.scan_task.cancel.name','permissions.meta.scan_task.cancel.description','active');
