BEGIN;

ALTER TABLE system.service_principals
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    ADD CONSTRAINT service_principals_name_length_check
        CHECK (char_length(btrim(name)) <= 120),
    ADD CONSTRAINT service_principals_description_length_check
        CHECK (char_length(description) <= 500);

CREATE UNIQUE INDEX uq_tenant_service_principals_name
    ON system.service_principals (owner_tenant_id, lower(btrim(name)))
    WHERE owner_scope = 'tenant';

ALTER TABLE system.oauth_clients
    DROP CONSTRAINT oauth_clients_management_owner_check,
    ADD CONSTRAINT oauth_clients_management_owner_check CHECK (
        (owner_scope = 'platform' AND owner_tenant_id IS NULL)
        OR (owner_scope = 'tenant'
            AND owner_tenant_id IS NOT NULL
            AND created_by_principal_id IS NOT NULL
            AND (
                (client_id ~ '^addp_ext_[A-Za-z0-9_-]{16,64}$'
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
                OR
                (client_id ~ '^addp_svc_[A-Za-z0-9_-]{16,64}$'
                    AND client_type = 'confidential'
                    AND client_secret_hash IS NOT NULL
                    AND service_principal_id IS NOT NULL
                    AND redirect_uris = ARRAY[]::text[]
                    AND grant_types = ARRAY['client_credentials']::text[]
                    AND response_types = ARRAY[]::text[]
                    AND allowed_scopes = ARRAY['addp.api']::text[]
                    AND allowed_audiences = ARRAY['addp.api']::text[]
                    AND token_endpoint_auth_method = 'client_secret_basic'
                    AND jwks_uri IS NULL
                    AND jwks IS NULL
                    AND request_uris = ARRAY[]::text[]
                    AND id_token_signed_response_alg IS NULL)
            ))
    );

CREATE OR REPLACE FUNCTION system.validate_oauth_service_client()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    principal_status text;
    principal_owner_scope text;
    principal_owner_tenant_id bigint;
BEGIN
    IF NEW.service_principal_id IS NOT NULL THEN
        SELECT principal.status, service_principal.owner_scope, service_principal.owner_tenant_id
        INTO principal_status, principal_owner_scope, principal_owner_tenant_id
        FROM system.principals AS principal
        JOIN system.service_principals AS service_principal
          ON service_principal.id = principal.id
        WHERE principal.id = NEW.service_principal_id
          AND principal.principal_type = 'service_principal';

        IF principal_owner_scope IS NULL
           OR (NEW.status = 'active' AND principal_status <> 'active')
           OR principal_owner_scope <> NEW.owner_scope
           OR principal_owner_tenant_id IS DISTINCT FROM NEW.owner_tenant_id
           OR (principal_owner_scope = 'tenant' AND NEW.status = 'active' AND NOT EXISTS (
                SELECT 1
                FROM system.tenant_memberships AS membership
                WHERE membership.tenant_id = principal_owner_tenant_id
                  AND membership.principal_id = NEW.service_principal_id
                  AND membership.status = 'active'
                  AND (membership.expires_at IS NULL OR membership.expires_at > now())
           )) THEN
            RAISE EXCEPTION 'OAuth service client ownership and active identity must match its service principal'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF TG_OP = 'UPDATE'
       AND NEW.service_principal_id IS DISTINCT FROM OLD.service_principal_id THEN
        RAISE EXCEPTION 'OAuth client service principal binding is immutable'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION system.validate_tenant_service_principal_membership()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    service_owner_tenant_id bigint;
BEGIN
    SELECT owner_tenant_id
    INTO service_owner_tenant_id
    FROM system.service_principals
    WHERE id = NEW.principal_id
      AND owner_scope = 'tenant';

    IF service_owner_tenant_id IS NOT NULL
       AND NEW.tenant_id <> service_owner_tenant_id THEN
        RAISE EXCEPTION 'tenant-owned service principal cannot join another tenant'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_tenant_memberships_validate_service_principal_owner
BEFORE INSERT OR UPDATE OF tenant_id, principal_id ON system.tenant_memberships
FOR EACH ROW EXECUTE FUNCTION system.validate_tenant_service_principal_membership();

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
    ('iam.service_account.create', 'create', 'high'),
    ('iam.service_account.read', 'read', 'low'),
    ('iam.service_account.restore', 'restore', 'high'),
    ('iam.service_credential.update', 'update', 'high'),
    ('iam.service_account.suspend', 'suspend', 'high'),
    ('iam.service_account.update', 'update', 'medium')
) AS seed(permission_key, action, risk_level)
ORDER BY permission_key;

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'iam.service_account.create',
      'iam.service_account.read',
      'iam.service_account.restore',
      'iam.service_credential.update',
      'iam.service_account.suspend',
      'iam.service_account.update'
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
