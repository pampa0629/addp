BEGIN;

ALTER TABLE system.execution_authorizations
    DROP CONSTRAINT execution_authorizations_audience_check;

UPDATE system.execution_authorizations
SET audience = 'quality'
WHERE audience = 'addp-quality';

ALTER TABLE system.execution_authorizations
    ADD CONSTRAINT execution_authorizations_audience_check
        CHECK (audience IN ('develop', 'duckdb', 'model', 'quality', 'service'));

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES (
    'model.materialization.execute', 'model', 'execute', 'critical', false,
    ARRAY['tenant']::text[], true,
    'permissions.model.materialization.execute.name',
    'permissions.model.materialization.execute.description', 'active'
)
ON CONFLICT (permission_key) DO NOTHING;

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM (VALUES
    ('tenant.data_architect', 'develop.data_ddl.execute'),
    ('tenant.data_architect', 'develop.data_read.execute'),
    ('tenant.data_architect', 'model.materialization.execute'),
    ('tenant.data_architect', 'system.execution_authorization.create'),
    ('tenant.model_runtime', 'system.engine_descriptor.read'),
    ('tenant.model_runtime', 'system.execution_authorization.execute')
) AS seed(role_key, permission_key)
JOIN system.roles AS role
  ON role.tenant_id IS NULL
 AND role.role_key = seed.role_key
 AND role.status = 'active'
JOIN system.permissions AS permission
  ON permission.permission_key = seed.permission_key
 AND permission.status = 'active'
ORDER BY seed.role_key, seed.permission_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
