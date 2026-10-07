BEGIN;

-- Publish explicit capabilities only. No Role Assignment, delegation or Grant.
INSERT INTO system.permissions (permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status)
VALUES
    ('system.engine_access_grant.create', 'system', 'create', 'high', false,
        ARRAY['tenant']::text[], true, 'permissions.system.engine_access_grant.create.name',
        'permissions.system.engine_access_grant.create.description', 'active'),
    ('system.engine_access_grant.read', 'system', 'read', 'low', false,
        ARRAY['tenant']::text[], true, 'permissions.system.engine_access_grant.read.name',
        'permissions.system.engine_access_grant.read.description', 'active');

COMMIT;
