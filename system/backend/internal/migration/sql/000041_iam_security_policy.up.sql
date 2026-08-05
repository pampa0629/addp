BEGIN;

CREATE TABLE system.iam_security_policy (
    id smallint PRIMARY KEY CHECK (id = 1),
    version bigint NOT NULL CHECK (version > 0),
    applied_version bigint NOT NULL CHECK (applied_version >= 0 AND applied_version <= version),
    access_token_ttl_minutes integer NOT NULL CHECK (access_token_ttl_minutes BETWEEN 1 AND 60),
    delegated_access_token_ttl_minutes integer NOT NULL CHECK (delegated_access_token_ttl_minutes BETWEEN 1 AND 2),
    resource_access_ticket_ttl_minutes integer NOT NULL CHECK (
        resource_access_ticket_ttl_minutes BETWEEN 1 AND 60
        AND resource_access_ticket_ttl_minutes <= access_token_ttl_minutes
    ),
    refresh_token_ttl_days integer NOT NULL CHECK (refresh_token_ttl_days BETWEEN 1 AND 365),
    oauth_authorization_code_ttl_minutes integer NOT NULL CHECK (oauth_authorization_code_ttl_minutes BETWEEN 1 AND 5),
    oauth_device_code_ttl_minutes integer NOT NULL CHECK (oauth_device_code_ttl_minutes BETWEEN 5 AND 30),
    oauth_device_poll_interval_seconds integer NOT NULL CHECK (oauth_device_poll_interval_seconds BETWEEN 5 AND 60),
    tenant_invitation_ttl_hours integer NOT NULL CHECK (tenant_invitation_ttl_hours BETWEEN 1 AND 720),
    enrollment_ticket_ttl_minutes integer NOT NULL CHECK (enrollment_ticket_ttl_minutes BETWEEN 1 AND 30),
    oauth_public_rate_limit_per_minute integer NOT NULL CHECK (oauth_public_rate_limit_per_minute BETWEEN 1 AND 10000),
    oauth_user_rate_limit_per_minute integer NOT NULL CHECK (oauth_user_rate_limit_per_minute BETWEEN 1 AND 10000),
    updated_by_principal_id bigint REFERENCES system.principals(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_iam_security_policy_updated_by_principal_id
    ON system.iam_security_policy (updated_by_principal_id);

INSERT INTO system.iam_security_policy (
    id,
    version,
    applied_version,
    access_token_ttl_minutes,
    delegated_access_token_ttl_minutes,
    resource_access_ticket_ttl_minutes,
    refresh_token_ttl_days,
    oauth_authorization_code_ttl_minutes,
    oauth_device_code_ttl_minutes,
    oauth_device_poll_interval_seconds,
    tenant_invitation_ttl_hours,
    enrollment_ticket_ttl_minutes,
    oauth_public_rate_limit_per_minute,
    oauth_user_rate_limit_per_minute
) VALUES (1, 1, 0, 15, 2, 15, 30, 5, 10, 5, 168, 5, 60, 30);

UPDATE system.permissions
SET status = 'active',
    updated_at = now()
WHERE permission_key IN (
    'iam.security_policy.read',
    'iam.security_policy.update'
)
  AND status = 'disabled';

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'iam.security_policy.read',
      'iam.security_policy.update'
  )
WHERE role.tenant_id IS NULL
  AND role.role_key = 'platform.security_administrator'
  AND role.role_type = 'platform_builtin'
  AND role.status = 'active'
  AND permission.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
