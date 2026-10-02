BEGIN;

-- Human handling is independent of business confirmation and machine recovery.
-- No builtin Role binding, existing assignment or content Grant is created.
INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status
) VALUES (
    'system.engine_access_fulfillment.create', 'system', 'create', 'high', false,
    ARRAY['tenant']::text[], true,
    'permissions.system.engine_access_fulfillment.create.name',
    'permissions.system.engine_access_fulfillment.create.description', 'active'
);

COMMIT;
