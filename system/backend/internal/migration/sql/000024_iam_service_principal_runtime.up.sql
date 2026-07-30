BEGIN;

CREATE OR REPLACE FUNCTION system.valid_oauth_grant_types(values_to_check text[])
RETURNS boolean
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT system.valid_distinct_text_array(values_to_check, true)
       AND values_to_check <@ ARRAY[
           'authorization_code',
           'client_credentials',
           'refresh_token',
           'urn:ietf:params:oauth:grant-type:device_code'
       ]::text[];
$$;

ALTER TABLE system.oauth_clients
    ADD COLUMN service_principal_id bigint UNIQUE REFERENCES system.service_principals(id);

ALTER TABLE system.oauth_clients
    DROP CONSTRAINT oauth_clients_redirect_uris_check,
    DROP CONSTRAINT oauth_clients_response_types_check,
    DROP CONSTRAINT oauth_clients_check1;

ALTER TABLE system.oauth_clients
    ADD CONSTRAINT oauth_clients_redirect_uris_check
        CHECK (system.valid_distinct_text_array(redirect_uris, false)),
    ADD CONSTRAINT oauth_clients_response_types_check
        CHECK (
            system.valid_distinct_text_array(response_types, false)
            AND response_types <@ ARRAY['code']::text[]
        ),
    ADD CONSTRAINT oauth_clients_credential_check CHECK (
        (client_type = 'public'
            AND client_secret_hash IS NULL
            AND token_endpoint_auth_method = 'none')
        OR (client_type = 'confidential'
            AND token_endpoint_auth_method = 'client_secret_basic'
            AND (client_secret_hash IS NOT NULL OR status = 'disabled'))
        OR (client_type = 'confidential'
            AND token_endpoint_auth_method = 'private_key_jwt'
            AND client_secret_hash IS NULL
            AND (jwks_uri IS NOT NULL OR jwks IS NOT NULL))
    ),
    ADD CONSTRAINT oauth_clients_service_principal_check CHECK (
        (service_principal_id IS NULL AND NOT ('client_credentials' = ANY(grant_types)))
        OR (service_principal_id IS NOT NULL
            AND client_type = 'confidential'
            AND token_endpoint_auth_method = 'client_secret_basic'
            AND redirect_uris = ARRAY[]::text[]
            AND grant_types = ARRAY['client_credentials']::text[]
            AND response_types = ARRAY[]::text[]
            AND allowed_scopes = ARRAY['addp.api']::text[]
            AND allowed_audiences = ARRAY['addp.api']::text[])
    );

CREATE FUNCTION system.validate_oauth_service_client()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.service_principal_id IS NOT NULL AND NOT EXISTS (
        SELECT 1
        FROM system.principals principal
        JOIN system.service_principals service_principal ON service_principal.id = principal.id
        WHERE principal.id = NEW.service_principal_id
          AND principal.principal_type = 'service_principal'
          AND principal.status = 'active'
    ) THEN
        RAISE EXCEPTION 'OAuth service client requires an active service principal'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'UPDATE'
       AND NEW.service_principal_id IS DISTINCT FROM OLD.service_principal_id THEN
        RAISE EXCEPTION 'OAuth client service principal binding is immutable'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_oauth_clients_validate_service_principal
BEFORE INSERT OR UPDATE ON system.oauth_clients
FOR EACH ROW EXECUTE FUNCTION system.validate_oauth_service_client();

CREATE UNIQUE INDEX uq_platform_service_principals_name
    ON system.service_principals (name)
    WHERE owner_scope = 'platform';

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
)
SELECT seed.role_key, 'roles.' || seed.role_key || '.name',
       'roles.' || seed.role_key || '.description', 'tenant_builtin',
       ARRAY['tenant']::text[], ARRAY['service_principal']::text[], true, 'active'
FROM (VALUES
    ('tenant.develop_runtime'),
    ('tenant.manager_runtime'),
    ('tenant.monitor_runtime'),
    ('tenant.quality_runtime'),
    ('tenant.service_runtime'),
    ('tenant.transfer_runtime')
) AS seed(role_key)
ORDER BY seed.role_key;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM (VALUES
    ('tenant.develop_runtime', 'meta.catalog.read'),
    ('tenant.develop_runtime', 'meta.scan_task.execute'),
    ('tenant.manager_runtime', 'meta.catalog.read'),
    ('tenant.manager_runtime', 'meta.scan_task.execute'),
    ('tenant.monitor_runtime', 'meta.scan_task.read'),
    ('tenant.quality_runtime', 'meta.catalog.read'),
    ('tenant.service_runtime', 'meta.catalog.read'),
    ('tenant.transfer_runtime', 'meta.catalog.read'),
    ('tenant.transfer_runtime', 'meta.inspect.execute'),
    ('tenant.transfer_runtime', 'meta.scan_task.execute')
) AS seed(role_key, permission_key)
JOIN system.roles role ON role.tenant_id IS NULL AND role.role_key = seed.role_key
JOIN system.permissions permission ON permission.permission_key = seed.permission_key
ORDER BY seed.role_key, seed.permission_key;

DO $$
DECLARE
    service_name text;
    service_description text;
    service_id bigint;
BEGIN
    FOR service_name, service_description IN
        SELECT * FROM (VALUES
            ('addp-develop', 'ADDP Develop runtime'),
            ('addp-manager', 'ADDP Manager runtime'),
            ('addp-monitor', 'ADDP Monitor runtime'),
            ('addp-quality', 'ADDP Quality runtime'),
            ('addp-service', 'ADDP Service runtime'),
            ('addp-transfer', 'ADDP Transfer runtime')
        ) AS seed(name, description)
        ORDER BY name
    LOOP
        INSERT INTO system.principals (principal_type, status)
        VALUES ('service_principal', 'active')
        RETURNING id INTO service_id;

        INSERT INTO system.service_principals (
            id, name, description, owner_scope, created_by_principal_id
        ) VALUES (
            service_id, service_name, service_description, 'platform', service_id
        );
    END LOOP;
END;
$$;

INSERT INTO system.oauth_clients (
    client_id, display_name, client_type, client_secret_hash, service_principal_id,
    redirect_uris, grant_types, response_types, allowed_scopes, allowed_audiences,
    token_endpoint_auth_method, status
)
SELECT service_principal.name, service_principal.description, 'confidential', NULL,
       service_principal.id, ARRAY[]::text[], ARRAY['client_credentials']::text[],
       ARRAY[]::text[], ARRAY['addp.api']::text[], ARRAY['addp.api']::text[],
       'client_secret_basic', 'disabled'
FROM system.service_principals service_principal
WHERE service_principal.owner_scope = 'platform'
  AND service_principal.name IN (
      'addp-develop', 'addp-manager', 'addp-monitor',
      'addp-quality', 'addp-service', 'addp-transfer'
  )
ORDER BY service_principal.name;

INSERT INTO system.tenant_memberships (
    tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id
)
SELECT tenant.id, service_principal.id, 'active', 'bootstrap', tenant.initialized_at,
       tenant.initialized_by_principal_id
FROM system.tenants tenant
CROSS JOIN system.service_principals service_principal
WHERE tenant.initialized_at IS NOT NULL
  AND service_principal.name IN (
      'addp-develop', 'addp-manager', 'addp-monitor',
      'addp-quality', 'addp-service', 'addp-transfer'
  )
ORDER BY tenant.id, service_principal.name;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, tenant_id, status, valid_from,
    source_type, reason
)
SELECT service_principal.id, role.id, 'tenant', tenant.id, 'active',
       tenant.initialized_at, 'bootstrap', 'built-in service runtime'
FROM system.tenants tenant
JOIN system.service_principals service_principal ON service_principal.name IN (
    'addp-develop', 'addp-manager', 'addp-monitor',
    'addp-quality', 'addp-service', 'addp-transfer'
)
JOIN system.roles role ON role.role_key =
    'tenant.' || replace(service_principal.name, 'addp-', '') || '_runtime'
WHERE tenant.initialized_at IS NOT NULL
ORDER BY tenant.id, service_principal.name;

COMMIT;
