BEGIN;

ALTER TABLE system.execution_authorizations
    DROP CONSTRAINT execution_authorizations_audience_check,
    ADD COLUMN source_type text NOT NULL DEFAULT 'user',
    ADD COLUMN source_definition_id bigint,
    ADD COLUMN source_definition_version text,
    ADD CONSTRAINT execution_authorizations_audience_check
        CHECK (audience IN ('develop', 'duckdb')),
    ADD CONSTRAINT execution_authorizations_source_check CHECK (
        (source_type = 'user'
            AND source_definition_id IS NULL
            AND source_definition_version IS NULL)
        OR
        (source_type = 'service_definition'
            AND source_definition_id > 0
            AND source_definition_version ~ '^[0-9a-f]{64}$')
    );

ALTER TABLE system.execution_authorizations
    ALTER COLUMN source_type DROP DEFAULT;

DROP TRIGGER trg_execution_authorizations_validate ON system.execution_authorizations;
DROP TRIGGER trg_execution_authorizations_validate_update ON system.execution_authorizations;
DROP FUNCTION system.validate_execution_authorization();
DROP FUNCTION system.validate_execution_authorization_update();

CREATE FUNCTION system.validate_execution_authorization()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    invalid_engine boolean;
BEGIN
    IF NEW.source_type = 'user' THEN
        IF NOT EXISTS (
            SELECT 1
            FROM system.principals principal
            JOIN system.tenant_memberships membership
              ON membership.id = NEW.tenant_membership_id
             AND membership.principal_id = principal.id
             AND membership.tenant_id = NEW.tenant_id
            JOIN system.tenants tenant ON tenant.id = membership.tenant_id
            WHERE principal.id = NEW.actor_principal_id
              AND principal.principal_type = 'user'
              AND principal.status = 'active'
              AND principal.authorization_version = NEW.issued_authorization_version
              AND membership.status = 'active'
              AND (membership.expires_at IS NULL OR membership.expires_at > NEW.created_at)
              AND tenant.status = 'active'
            FOR KEY SHARE OF principal, membership, tenant
        ) THEN
            RAISE EXCEPTION 'user execution authorization requires an active tenant user subject'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.source_type = 'service_definition' THEN
        IF NEW.audience <> 'duckdb'
           OR NEW.effects <> ARRAY['read']::text[]
           OR NEW.expires_at > NEW.created_at + interval '1 minute'
           OR NOT EXISTS (
                SELECT 1
                FROM system.principals principal
                JOIN system.service_principals service_principal ON service_principal.id = principal.id
                JOIN system.tenant_memberships membership
                  ON membership.id = NEW.tenant_membership_id
                 AND membership.principal_id = principal.id
                 AND membership.tenant_id = NEW.tenant_id
                JOIN system.tenants tenant ON tenant.id = membership.tenant_id
                WHERE principal.id = NEW.actor_principal_id
                  AND principal.principal_type = 'service_principal'
                  AND principal.status = 'active'
                  AND principal.authorization_version = NEW.issued_authorization_version
                  AND service_principal.name = 'addp-service'
                  AND service_principal.owner_scope = 'platform'
                  AND membership.status = 'active'
                  AND (membership.expires_at IS NULL OR membership.expires_at > NEW.created_at)
                  AND tenant.status = 'active'
                FOR KEY SHARE OF principal, service_principal, membership, tenant
           ) THEN
            RAISE EXCEPTION 'service definition authorization requires the active addp-service tenant runtime and a read-only DuckDB boundary'
                USING ERRCODE = '23514';
        END IF;
    ELSE
        RAISE EXCEPTION 'unsupported execution authorization source type'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.expires_at <= NEW.created_at
       OR NEW.expires_at > NEW.created_at + interval '1 hour' THEN
        RAISE EXCEPTION 'invalid execution authorization expiry'
            USING ERRCODE = '23514';
    END IF;

    SELECT EXISTS (
        SELECT 1
        FROM unnest(NEW.engine_ids) AS requested(engine_id)
        WHERE NOT EXISTS (
            SELECT 1
            FROM system.engines engine
            WHERE engine.id = requested.engine_id
              AND engine.lifecycle_state = 'active'
              AND (
                  engine.tenant_id = NEW.tenant_id
                  OR (engine.tenant_id IS NULL AND engine.is_builtin = true)
              )
        )
    ) INTO invalid_engine;

    IF invalid_engine THEN
        RAISE EXCEPTION 'execution authorization contains an unavailable or cross-tenant engine'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION system.validate_execution_authorization_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.actor_principal_id IS DISTINCT FROM OLD.actor_principal_id
       OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.tenant_membership_id IS DISTINCT FROM OLD.tenant_membership_id
       OR NEW.issued_authorization_version IS DISTINCT FROM OLD.issued_authorization_version
       OR NEW.source_type IS DISTINCT FROM OLD.source_type
       OR NEW.source_definition_id IS DISTINCT FROM OLD.source_definition_id
       OR NEW.source_definition_version IS DISTINCT FROM OLD.source_definition_version
       OR NEW.execution_id IS DISTINCT FROM OLD.execution_id
       OR NEW.audience IS DISTINCT FROM OLD.audience
       OR NEW.effects IS DISTINCT FROM OLD.effects
       OR NEW.engine_ids IS DISTINCT FROM OLD.engine_ids
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'execution authorization identity and boundary are immutable'
            USING ERRCODE = '23514';
    END IF;
    IF OLD.revoked_at IS NOT NULL
       AND (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
            OR NEW.revoked_reason IS DISTINCT FROM OLD.revoked_reason) THEN
        RAISE EXCEPTION 'execution authorization revocation is immutable'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_execution_authorizations_validate
BEFORE INSERT ON system.execution_authorizations
FOR EACH ROW EXECUTE FUNCTION system.validate_execution_authorization();

CREATE TRIGGER trg_execution_authorizations_validate_update
BEFORE UPDATE ON system.execution_authorizations
FOR EACH ROW EXECUTE FUNCTION system.validate_execution_authorization_update();

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
)
VALUES (
    'tenant.duckdb_runtime', 'roles.tenant.duckdb_runtime.name',
    'roles.tenant.duckdb_runtime.description', 'tenant_builtin',
    ARRAY['tenant']::text[], ARRAY['service_principal']::text[], true, 'active'
);

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM (VALUES
    ('system.execution_authorization.execute'),
    ('meta.catalog.read')
) AS seed(permission_key)
JOIN system.roles role
  ON role.tenant_id IS NULL AND role.role_key = 'tenant.duckdb_runtime'
JOIN system.permissions permission
  ON permission.permission_key = seed.permission_key
ORDER BY seed.permission_key;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key = 'system.execution_authorization.execute'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.service_runtime';

WITH principal AS (
    INSERT INTO system.principals (principal_type, status)
    VALUES ('service_principal', 'active')
    RETURNING id
), service_principal AS (
    INSERT INTO system.service_principals (
        id, name, description, owner_scope, created_by_principal_id
    )
    SELECT id, 'addp-duckdb', 'ADDP DuckDB federated query runtime', 'platform', id
    FROM principal
    RETURNING id
)
INSERT INTO system.oauth_clients (
    client_id, display_name, client_type, client_secret_hash, service_principal_id,
    redirect_uris, grant_types, response_types, allowed_scopes, allowed_audiences,
    token_endpoint_auth_method, status
)
SELECT 'addp-duckdb', 'ADDP DuckDB federated query runtime', 'confidential', NULL, id,
       ARRAY[]::text[], ARRAY['client_credentials']::text[], ARRAY[]::text[],
       ARRAY['addp.api']::text[], ARRAY['addp.api']::text[],
       'client_secret_basic', 'disabled'
FROM service_principal;

INSERT INTO system.tenant_memberships (
    tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id
)
SELECT tenant.id, service_principal.id, 'active', 'bootstrap', tenant.initialized_at,
       tenant.initialized_by_principal_id
FROM system.tenants tenant
JOIN system.service_principals service_principal
  ON service_principal.name = 'addp-duckdb'
WHERE tenant.initialized_at IS NOT NULL
ORDER BY tenant.id;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, tenant_id, status, valid_from,
    source_type, reason
)
SELECT service_principal.id, role.id, 'tenant', tenant.id, 'active',
       tenant.initialized_at, 'bootstrap', 'built-in DuckDB query runtime'
FROM system.tenants tenant
JOIN system.service_principals service_principal
  ON service_principal.name = 'addp-duckdb'
JOIN system.roles role
  ON role.tenant_id IS NULL AND role.role_key = 'tenant.duckdb_runtime'
WHERE tenant.initialized_at IS NOT NULL
ORDER BY tenant.id;

COMMIT;
