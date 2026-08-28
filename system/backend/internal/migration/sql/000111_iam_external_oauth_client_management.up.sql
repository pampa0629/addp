BEGIN;

ALTER TABLE system.oauth_clients
    ADD COLUMN owner_scope text NOT NULL DEFAULT 'platform'
        CHECK (owner_scope IN ('platform', 'tenant')),
    ADD COLUMN owner_tenant_id bigint REFERENCES system.tenants(id),
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    ADD COLUMN created_by_principal_id bigint REFERENCES system.principals(id);

ALTER TABLE system.oauth_clients
    ADD CONSTRAINT oauth_clients_management_owner_check CHECK (
        (owner_scope = 'platform' AND owner_tenant_id IS NULL)
        OR (owner_scope = 'tenant'
            AND owner_tenant_id IS NOT NULL
            AND created_by_principal_id IS NOT NULL
            AND client_id ~ '^addp_ext_[A-Za-z0-9_-]{16,64}$'
            AND client_type = 'public'
            AND client_secret_hash IS NULL
            AND service_principal_id IS NULL
            AND cardinality(redirect_uris) > 0
            AND grant_types = ARRAY['authorization_code', 'refresh_token']::text[]
            AND response_types = ARRAY['code']::text[]
            AND allowed_scopes = ARRAY['addp.api']::text[]
            AND allowed_audiences = ARRAY['addp.api']::text[]
            AND token_endpoint_auth_method = 'none'
            AND jwks_uri IS NULL
            AND jwks IS NULL
            AND request_uris = ARRAY[]::text[]
            AND id_token_signed_response_alg IS NULL)
    );

CREATE INDEX idx_oauth_clients_tenant_management
    ON system.oauth_clients (owner_tenant_id, status, updated_at DESC, client_id)
    WHERE owner_scope = 'tenant';

CREATE INDEX idx_oauth_clients_created_by_principal
    ON system.oauth_clients (created_by_principal_id)
    WHERE created_by_principal_id IS NOT NULL;

CREATE OR REPLACE FUNCTION system.validate_oauth_client_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.client_id <> OLD.client_id
       OR NEW.owner_scope <> OLD.owner_scope
       OR NEW.owner_tenant_id IS DISTINCT FROM OLD.owner_tenant_id
       OR NEW.created_by_principal_id IS DISTINCT FROM OLD.created_by_principal_id
       OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'OAuth client identity and management ownership are immutable'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT permission_key, 'system', action, risk_level, false,
       ARRAY['tenant']::text[], false,
       'permissions.' || permission_key || '.name',
       'permissions.' || permission_key || '.description', 'active'
FROM (VALUES
    ('iam.oauth_client.create', 'create', 'medium'),
    ('iam.oauth_client.read', 'read', 'low'),
    ('iam.oauth_client.restore', 'restore', 'high'),
    ('iam.oauth_client.suspend', 'suspend', 'high'),
    ('iam.oauth_client.update', 'update', 'medium')
) AS seed(permission_key, action, risk_level)
ORDER BY permission_key;

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'iam.oauth_client.create',
      'iam.oauth_client.read',
      'iam.oauth_client.restore',
      'iam.oauth_client.suspend',
      'iam.oauth_client.update'
  )
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.administrator'
  AND role.status = 'active'
ORDER BY permission.permission_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

WITH affected_principals AS (
    SELECT DISTINCT assignment.principal_id
    FROM system.role_assignments AS assignment
    JOIN system.roles AS role ON role.id = assignment.role_id
    WHERE assignment.status = 'active'
      AND role.tenant_id IS NULL
      AND role.role_key = 'tenant.administrator'
)
UPDATE system.principals AS principal
SET authorization_version = principal.authorization_version + 1,
    updated_at = now()
FROM affected_principals AS affected
WHERE principal.id = affected.principal_id;

COMMIT;
