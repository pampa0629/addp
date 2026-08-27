BEGIN;

ALTER TABLE system.execution_authorizations
    ADD COLUMN sealed_at timestamptz;

CREATE TABLE system.execution_authorization_engine_accesses (
    authorization_id bigint NOT NULL REFERENCES system.execution_authorizations(id),
    engine_id bigint NOT NULL,
    effects text[] NOT NULL CHECK (
        system.valid_distinct_text_array(effects, true)
        AND effects <@ ARRAY['read', 'write', 'ddl', 'external_effect']::text[]
    ),
    PRIMARY KEY (authorization_id, engine_id),
    CHECK (engine_id > 0)
);

INSERT INTO system.execution_authorization_engine_accesses (
    authorization_id,
    engine_id,
    effects
)
SELECT execution_authorization.id, requested.engine_id, execution_authorization.effects
FROM system.execution_authorizations AS execution_authorization
CROSS JOIN LATERAL unnest(execution_authorization.engine_ids) AS requested(engine_id);

UPDATE system.execution_authorizations
SET sealed_at = created_at;

DROP TRIGGER IF EXISTS trg_execution_authorizations_validate
    ON system.execution_authorizations;
DROP TRIGGER IF EXISTS trg_execution_authorizations_validate_update
    ON system.execution_authorizations;
DROP FUNCTION IF EXISTS system.validate_execution_authorization();
DROP FUNCTION IF EXISTS system.validate_execution_authorization_update();

ALTER TABLE system.execution_authorizations
    DROP COLUMN effects,
    DROP COLUMN engine_ids;

CREATE FUNCTION system.validate_execution_authorization()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.sealed_at IS NOT NULL THEN
        RAISE EXCEPTION 'new execution authorization must be unsealed'
            USING ERRCODE = '23514';
    END IF;

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

        IF NEW.source_notebook_session_authorization_id IS NOT NULL
           AND NOT EXISTS (
                SELECT 1
                FROM system.notebook_session_authorizations session_authorization
                JOIN system.refresh_token_families family
                  ON family.id = session_authorization.token_family_id
                WHERE session_authorization.id = NEW.source_notebook_session_authorization_id
                  AND session_authorization.actor_principal_id = NEW.actor_principal_id
                  AND session_authorization.tenant_id = NEW.tenant_id
                  AND session_authorization.tenant_membership_id = NEW.tenant_membership_id
                  AND session_authorization.issued_authorization_version = NEW.issued_authorization_version
                  AND session_authorization.audience = 'develop'
                  AND session_authorization.revoked_at IS NULL
                  AND session_authorization.expires_at > NEW.created_at
                  AND session_authorization.expires_at >= NEW.expires_at
                  AND family.revoked_at IS NULL
                  AND family.expires_at > NEW.created_at
                FOR KEY SHARE OF session_authorization, family
           ) THEN
            RAISE EXCEPTION 'notebook execution authorization requires an active matching notebook session authorization'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.source_type = 'service_definition' THEN
        IF NEW.source_notebook_session_authorization_id IS NOT NULL
           OR NEW.audience <> 'duckdb'
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
            RAISE EXCEPTION 'service definition authorization requires the active addp-service tenant runtime and a DuckDB boundary'
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
       OR NEW.source_notebook_session_authorization_id IS DISTINCT FROM OLD.source_notebook_session_authorization_id
       OR NEW.source_execution_attempt IS DISTINCT FROM OLD.source_execution_attempt
       OR NEW.source_execution_lease_token IS DISTINCT FROM OLD.source_execution_lease_token
       OR NEW.execution_id IS DISTINCT FROM OLD.execution_id
       OR NEW.audience IS DISTINCT FROM OLD.audience
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'execution authorization identity and boundary are immutable'
            USING ERRCODE = '23514';
    END IF;

    IF OLD.sealed_at IS NULL AND NEW.sealed_at IS NOT NULL THEN
        IF NEW.sealed_at IS DISTINCT FROM NEW.created_at
           OR NOT EXISTS (
                SELECT 1
                FROM system.execution_authorization_engine_accesses access
                WHERE access.authorization_id = NEW.id
           ) THEN
            RAISE EXCEPTION 'execution authorization sealing requires a non-empty immutable access boundary'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.sealed_at IS DISTINCT FROM OLD.sealed_at THEN
        RAISE EXCEPTION 'execution authorization seal is immutable'
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

CREATE FUNCTION system.validate_execution_authorization_engine_access_insert()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    current_authorization system.execution_authorizations%ROWTYPE;
BEGIN
    SELECT * INTO current_authorization
    FROM system.execution_authorizations
    WHERE id = NEW.authorization_id
    FOR UPDATE;

    IF current_authorization.id IS NULL OR current_authorization.sealed_at IS NOT NULL THEN
        RAISE EXCEPTION 'execution authorization access boundary is already sealed'
            USING ERRCODE = '23514';
    END IF;
    IF current_authorization.source_type = 'service_definition'
       AND NEW.effects <> ARRAY['read']::text[] THEN
        RAISE EXCEPTION 'service definition execution authorization is read-only'
            USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM system.engines engine
        WHERE engine.id = NEW.engine_id
          AND engine.lifecycle_state = 'active'
          AND (
              engine.tenant_id = current_authorization.tenant_id
              OR (engine.tenant_id IS NULL AND engine.is_builtin = true)
          )
    ) THEN
        RAISE EXCEPTION 'execution authorization contains an unavailable or cross-tenant engine'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION system.prevent_execution_authorization_engine_access_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'execution authorization access boundary is immutable'
        USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER trg_execution_authorizations_validate
BEFORE INSERT ON system.execution_authorizations
FOR EACH ROW EXECUTE FUNCTION system.validate_execution_authorization();

CREATE TRIGGER trg_execution_authorizations_validate_update
BEFORE UPDATE ON system.execution_authorizations
FOR EACH ROW EXECUTE FUNCTION system.validate_execution_authorization_update();

CREATE TRIGGER trg_execution_authorization_engine_accesses_validate_insert
BEFORE INSERT ON system.execution_authorization_engine_accesses
FOR EACH ROW EXECUTE FUNCTION system.validate_execution_authorization_engine_access_insert();

CREATE TRIGGER trg_execution_authorization_engine_accesses_prevent_update
BEFORE UPDATE ON system.execution_authorization_engine_accesses
FOR EACH ROW EXECUTE FUNCTION system.prevent_execution_authorization_engine_access_mutation();

CREATE TRIGGER trg_execution_authorization_engine_accesses_prevent_delete
BEFORE DELETE ON system.execution_authorization_engine_accesses
FOR EACH ROW EXECUTE FUNCTION system.prevent_execution_authorization_engine_access_mutation();

CREATE INDEX idx_execution_authorization_engine_accesses_engine
    ON system.execution_authorization_engine_accesses (engine_id, authorization_id);

COMMIT;
