BEGIN;

ALTER TABLE system.execution_authorizations
    DROP CONSTRAINT execution_authorizations_audience_check,
    ADD CONSTRAINT execution_authorizations_audience_check
        CHECK (audience IN ('addp-quality', 'develop', 'duckdb', 'service'));

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM (VALUES
    ('tenant.governance_manager', 'develop.data_read.execute'),
    ('tenant.governance_manager', 'system.execution_authorization.create'),
    ('tenant.quality_runtime', 'system.execution_authorization.execute')
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
