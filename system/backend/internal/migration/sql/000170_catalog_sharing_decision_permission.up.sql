BEGIN;

-- Explicit business confirmation only, no builtin Role grant or data access.
INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status
) VALUES (
    'catalog.sharing_decision.create', 'catalog', 'create', 'high', false,
    ARRAY['tenant']::text[], true,
    'permissions.catalog.sharing_decision.create.name',
    'permissions.catalog.sharing_decision.create.description', 'active'
);

COMMIT;
