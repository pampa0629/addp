BEGIN;

ALTER TABLE system.execution_authorizations
    ADD COLUMN actor_principal_id bigint REFERENCES system.principals(id),
    ADD COLUMN tenant_id bigint REFERENCES system.tenants(id),
    ADD COLUMN tenant_membership_id bigint REFERENCES system.tenant_memberships(id),
    ADD COLUMN issued_authorization_version bigint;

UPDATE system.execution_authorizations AS execution_authorization
SET actor_principal_id = family.principal_id,
    tenant_id = membership.tenant_id,
    tenant_membership_id = membership.id,
    issued_authorization_version = family.issued_authorization_version
FROM system.access_tokens access_token
JOIN system.refresh_token_families family
  ON family.id = access_token.family_id
JOIN system.tenant_memberships membership
  ON membership.id = family.tenant_membership_id
WHERE access_token.id = execution_authorization.source_access_token_id;

ALTER TABLE system.execution_authorizations
    ALTER COLUMN actor_principal_id SET NOT NULL,
    ALTER COLUMN tenant_id SET NOT NULL,
    ALTER COLUMN tenant_membership_id SET NOT NULL,
    ALTER COLUMN issued_authorization_version SET NOT NULL,
    ADD CONSTRAINT execution_authorizations_authorization_version_check
        CHECK (issued_authorization_version > 0);

DROP TRIGGER trg_execution_authorizations_validate ON system.execution_authorizations;
DROP TRIGGER trg_execution_authorizations_validate_update ON system.execution_authorizations;
DROP FUNCTION system.validate_execution_authorization();
DROP FUNCTION system.validate_execution_authorization_update();
DROP INDEX system.idx_execution_authorizations_source_active;

CREATE OR REPLACE FUNCTION system.revoke_access_token_delegations()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL THEN
        UPDATE system.delegated_access_tokens
        SET revoked_at = NEW.revoked_at
        WHERE source_access_token_id = NEW.id
          AND revoked_at IS NULL;
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE system.execution_authorizations
    DROP COLUMN source_access_token_id;

CREATE FUNCTION system.validate_execution_authorization()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    invalid_engine boolean;
BEGIN
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
        RAISE EXCEPTION 'execution authorization requires an active tenant user subject'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.expires_at > NEW.created_at + interval '1 hour' THEN
        RAISE EXCEPTION 'execution authorization expiry exceeds one hour'
            USING ERRCODE = '23514';
    END IF;

    EXECUTE $query$
        SELECT EXISTS (
            SELECT 1
            FROM unnest($1::bigint[]) AS requested(engine_id)
            WHERE NOT EXISTS (
                SELECT 1
                FROM system.engines engine
                WHERE engine.id = requested.engine_id
                  AND engine.lifecycle_state = 'active'
                  AND (
                      engine.tenant_id = $2
                      OR (engine.tenant_id IS NULL AND engine.is_builtin = true)
                  )
            )
        )
    $query$
    INTO invalid_engine
    USING NEW.engine_ids, NEW.tenant_id;

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

CREATE INDEX idx_execution_authorizations_actor_active
    ON system.execution_authorizations (actor_principal_id, tenant_id, expires_at)
    WHERE revoked_at IS NULL;
CREATE INDEX idx_execution_authorizations_membership_active
    ON system.execution_authorizations (tenant_membership_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE system.task_authorization_subjects (
    id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    owner_module text NOT NULL CHECK (owner_module = 'orchestrator'),
    task_type text NOT NULL CHECK (task_type = 'orchestration'),
    task_ref uuid NOT NULL,
    definition_hash text NOT NULL CHECK (definition_hash ~ '^[0-9a-f]{64}$'),
    tenant_id bigint NOT NULL REFERENCES system.tenants(id),
    principal_id bigint NOT NULL REFERENCES system.principals(id),
    tenant_membership_id bigint NOT NULL REFERENCES system.tenant_memberships(id),
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    authorized_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_module, task_ref)
);

CREATE INDEX idx_task_authorization_subjects_tenant
    ON system.task_authorization_subjects (tenant_id, owner_module, task_type);
CREATE INDEX idx_task_authorization_subjects_principal
    ON system.task_authorization_subjects (principal_id, tenant_id);
CREATE INDEX idx_task_authorization_subjects_membership
    ON system.task_authorization_subjects (tenant_membership_id);

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES
    ('develop.task_provider.execute', 'develop', 'execute', 'medium', false,
     ARRAY['tenant']::text[], false,
     'permissions.develop.task_provider.execute.name',
     'permissions.develop.task_provider.execute.description', 'active'),
    ('develop.task_provider.read', 'develop', 'read', 'low', false,
     ARRAY['tenant']::text[], false,
     'permissions.develop.task_provider.read.name',
     'permissions.develop.task_provider.read.description', 'active'),
    ('system.runtime_registry.read', 'system', 'read', 'low', false,
     ARRAY['platform']::text[], false,
     'permissions.system.runtime_registry.read.name',
     'permissions.system.runtime_registry.read.description', 'active'),
    ('system.task_authorization.execute', 'system', 'execute', 'high', false,
     ARRAY['tenant']::text[], false,
     'permissions.system.task_authorization.execute.name',
     'permissions.system.task_authorization.execute.description', 'active');

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
) VALUES
    ('platform.orchestrator_runtime', 'roles.platform.orchestrator_runtime.name',
     'roles.platform.orchestrator_runtime.description', 'platform_builtin',
     ARRAY['platform']::text[], ARRAY['service_principal']::text[], true, 'active'),
    ('tenant.orchestrator_runtime', 'roles.tenant.orchestrator_runtime.name',
     'roles.tenant.orchestrator_runtime.description', 'tenant_builtin',
     ARRAY['tenant']::text[], ARRAY['service_principal']::text[], true, 'active');

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM (VALUES
    ('platform.orchestrator_runtime', 'system.runtime_registry.read'),
    ('platform.orchestrator_runtime', 'system.runtime_registry.update'),
    ('tenant.orchestrator_runtime', 'develop.task_provider.execute'),
    ('tenant.orchestrator_runtime', 'develop.task_provider.read'),
    ('tenant.orchestrator_runtime', 'system.task_authorization.execute')
) AS seed(role_key, permission_key)
JOIN system.roles role ON role.tenant_id IS NULL AND role.role_key = seed.role_key
JOIN system.permissions permission ON permission.permission_key = seed.permission_key;

WITH principal AS (
    INSERT INTO system.principals (principal_type, status)
    VALUES ('service_principal', 'active')
    RETURNING id
)
INSERT INTO system.service_principals (
    id, name, description, owner_scope, created_by_principal_id
)
SELECT id, 'addp-orchestrator', 'ADDP Orchestrator runtime', 'platform', id
FROM principal;

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
WHERE service_principal.name = 'addp-orchestrator';

INSERT INTO system.tenant_memberships (
    tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id
)
SELECT tenant.id, service_principal.id, 'active', 'bootstrap', tenant.initialized_at,
       tenant.initialized_by_principal_id
FROM system.tenants tenant
CROSS JOIN system.service_principals service_principal
WHERE tenant.initialized_at IS NOT NULL
  AND service_principal.name = 'addp-orchestrator';

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, tenant_id, status, valid_from,
    source_type, reason
)
SELECT service_principal.id, role.id, 'tenant', tenant.id, 'active',
       tenant.initialized_at, 'bootstrap', 'built-in service runtime'
FROM system.tenants tenant
JOIN system.service_principals service_principal ON service_principal.name = 'addp-orchestrator'
JOIN system.roles role ON role.tenant_id IS NULL AND role.role_key = 'tenant.orchestrator_runtime'
WHERE tenant.initialized_at IS NOT NULL;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, status, valid_from, source_type, reason
)
SELECT service_principal.id, role.id, 'platform', 'active', transaction_timestamp(),
       'bootstrap', 'built-in service control plane runtime'
FROM system.service_principals service_principal
JOIN system.roles role ON role.tenant_id IS NULL AND role.role_key = 'platform.orchestrator_runtime'
WHERE service_principal.name = 'addp-orchestrator';

COMMIT;
