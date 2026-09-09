BEGIN;

-- 旧 Application/API Key 从未形成数据面授权闭环，也错误地进入了通用控制面。
-- 当前产品尚未对外提供该能力，因此直接删除旧模型，不保留双读或兼容路由。
DROP TABLE IF EXISTS system.api_keys CASCADE;
DROP TABLE IF EXISTS system.applications CASCADE;

DELETE FROM system.role_permissions
WHERE permission_id IN (
    SELECT id
    FROM system.permissions
    WHERE permission_key IN (
        'system.application.create',
        'system.application.delete',
        'system.application.read',
        'system.application.update',
        'system.api_key.create',
        'system.api_key.read',
        'system.api_key.revoke'
    )
);

UPDATE system.permissions
SET status = 'disabled',
    updated_at = transaction_timestamp()
WHERE permission_key IN (
    'system.application.create',
    'system.application.delete',
    'system.application.read',
    'system.application.update',
    'system.api_key.create',
    'system.api_key.read',
    'system.api_key.revoke'
);

CREATE TABLE system.api_consumers (
    id bigserial PRIMARY KEY,
    tenant_id bigint NOT NULL REFERENCES system.tenants(id),
    name varchar(120) NOT NULL,
    description varchar(500) NOT NULL DEFAULT '',
    rate_limit_per_minute integer NOT NULL DEFAULT 60
        CHECK (rate_limit_per_minute BETWEEN 1 AND 100000),
    status varchar(20) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'suspended')),
    created_by_principal_id bigint NOT NULL REFERENCES system.principals(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT api_consumers_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX uq_api_consumers_tenant_name
    ON system.api_consumers (tenant_id, lower(btrim(name)));

CREATE INDEX idx_api_consumers_created_by_principal
    ON system.api_consumers (created_by_principal_id);

CREATE TABLE system.api_consumer_service_grants (
    id bigserial PRIMARY KEY,
    api_consumer_id bigint NOT NULL REFERENCES system.api_consumers(id) ON DELETE CASCADE,
    service_type varchar(20) NOT NULL CHECK (service_type IN ('query')),
    service_id bigint NOT NULL CHECK (service_id > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (api_consumer_id, service_type, service_id)
);

CREATE TABLE system.api_consumer_credentials (
    id bigserial PRIMARY KEY,
    api_consumer_id bigint NOT NULL REFERENCES system.api_consumers(id) ON DELETE CASCADE,
    key_prefix varchar(20) NOT NULL,
    key_hash char(64) NOT NULL UNIQUE,
    name varchar(120) NOT NULL DEFAULT '',
    status varchar(20) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'revoked')),
    last_used_at timestamptz,
    expires_at timestamptz,
    created_by_principal_id bigint NOT NULL REFERENCES system.principals(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    revoked_by_principal_id bigint REFERENCES system.principals(id),
    CONSTRAINT api_consumer_credentials_expiry_check
        CHECK (expires_at IS NULL OR expires_at > created_at)
);

CREATE INDEX idx_api_consumer_credentials_consumer
    ON system.api_consumer_credentials (api_consumer_id, created_at DESC);

CREATE INDEX idx_api_consumer_credentials_created_by_principal
    ON system.api_consumer_credentials (created_by_principal_id);

CREATE INDEX idx_api_consumer_credentials_revoked_by_principal
    ON system.api_consumer_credentials (revoked_by_principal_id)
    WHERE revoked_by_principal_id IS NOT NULL;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT permission_key, 'system', action, risk_level, false,
       allowed_scope_types, false,
       'permissions.' || permission_key || '.name',
       'permissions.' || permission_key || '.description', 'active'
FROM (VALUES
    ('iam.api_consumer.create', 'create', 'high', ARRAY['tenant']::text[]),
    ('iam.api_consumer.delete', 'delete', 'high', ARRAY['tenant']::text[]),
    ('iam.api_consumer.read', 'read', 'low', ARRAY['tenant']::text[]),
    ('iam.api_consumer.update', 'update', 'medium', ARRAY['tenant']::text[]),
    ('iam.api_consumer_credential.create', 'create', 'high', ARRAY['tenant']::text[]),
    ('iam.api_consumer_credential.read', 'read', 'low', ARRAY['tenant']::text[]),
    ('iam.api_consumer_credential.revoke', 'revoke', 'high', ARRAY['tenant']::text[]),
    ('iam.api_consumer_runtime.read', 'read', 'low', ARRAY['platform']::text[])
) AS seed(permission_key, action, risk_level, allowed_scope_types)
ORDER BY permission_key;

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'iam.api_consumer.create',
      'iam.api_consumer.delete',
      'iam.api_consumer.read',
      'iam.api_consumer.update',
      'iam.api_consumer_credential.create',
      'iam.api_consumer_credential.read',
      'iam.api_consumer_credential.revoke',
      'service.definition.read'
  )
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.administrator'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'iam.api_consumer_runtime.read'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key IN ('platform.gateway_runtime', 'platform.service_runtime')
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

WITH affected_principals AS (
    SELECT DISTINCT assignment.principal_id
    FROM system.role_assignments AS assignment
    JOIN system.roles AS role ON role.id = assignment.role_id
    WHERE assignment.status = 'active'
      AND role.tenant_id IS NULL
      AND role.role_key IN (
          'tenant.administrator',
          'platform.gateway_runtime',
          'platform.service_runtime'
      )
)
UPDATE system.principals AS principal
SET authorization_version = principal.authorization_version + 1,
    updated_at = now()
FROM affected_principals AS affected
WHERE principal.id = affected.principal_id;

COMMIT;
