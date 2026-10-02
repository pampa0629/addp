BEGIN;

-- Explicit governance configuration, never a builtin Role or content grant.
INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status
) VALUES
    ('system.engine_access_approval_requirement.initialize', 'system', 'initialize', 'high', false,
     ARRAY['tenant']::text[], true,
     'permissions.system.engine_access_approval_requirement.initialize.name',
     'permissions.system.engine_access_approval_requirement.initialize.description', 'active'),
    ('system.engine_access_approval_requirement.read', 'system', 'read', 'low', false,
     ARRAY['tenant']::text[], true,
     'permissions.system.engine_access_approval_requirement.read.name',
     'permissions.system.engine_access_approval_requirement.read.description', 'active');

COMMIT;
