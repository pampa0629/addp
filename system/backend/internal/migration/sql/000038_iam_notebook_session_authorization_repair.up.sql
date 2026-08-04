BEGIN;

CREATE TABLE system.schema_migration_checksums (
    version bigint PRIMARY KEY CHECK (version > 0),
    filename text NOT NULL UNIQUE CHECK (btrim(filename) <> ''),
    sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    recorded_at timestamptz NOT NULL DEFAULT now()
);

DO $$
BEGIN
    IF to_regclass('system.notebook_catalog_authorizations') IS NOT NULL
       AND to_regclass('system.notebook_session_authorizations') IS NOT NULL THEN
        RAISE EXCEPTION 'legacy and canonical notebook authorization schemas coexist';
    END IF;

    IF to_regclass('system.notebook_catalog_authorizations') IS NOT NULL THEN
        DROP TRIGGER IF EXISTS trg_notebook_catalog_authorizations_validate
            ON system.notebook_catalog_authorizations;
        DROP TRIGGER IF EXISTS trg_notebook_catalog_authorizations_validate_update
            ON system.notebook_catalog_authorizations;
        DROP TRIGGER IF EXISTS trg_notebook_catalog_authorizations_prevent_delete
            ON system.notebook_catalog_authorizations;

        ALTER TABLE system.notebook_catalog_authorizations
            RENAME TO notebook_session_authorizations;
        ALTER TABLE system.notebook_session_authorizations
            RENAME COLUMN operation TO operations;
        ALTER TABLE system.notebook_session_authorizations
            DROP CONSTRAINT notebook_catalog_authorizations_operation_check;
        ALTER TABLE system.notebook_session_authorizations
            ALTER COLUMN operations TYPE text[]
            USING ARRAY['catalog.list_children', 'execution_engine_access.derive']::text[];
        ALTER TABLE system.notebook_session_authorizations
            ADD CONSTRAINT notebook_session_authorizations_operations_check CHECK (
                operations = ARRAY['catalog.list_children', 'execution_engine_access.derive']::text[]
            );

        ALTER TABLE system.notebook_session_authorizations
            RENAME CONSTRAINT notebook_catalog_authorizations_pkey
            TO notebook_session_authorizations_pkey;
        ALTER TABLE system.notebook_session_authorizations
            RENAME CONSTRAINT notebook_catalog_authorizations_session_id_key
            TO notebook_session_authorizations_session_id_key;
        ALTER TABLE system.notebook_session_authorizations
            RENAME CONSTRAINT notebook_catalog_authorizations_task_id_check
            TO notebook_session_authorizations_task_id_check;
        ALTER TABLE system.notebook_session_authorizations
            RENAME CONSTRAINT notebook_catalog_authorizati_issued_authorization_version_check
            TO notebook_session_authorizati_issued_authorization_version_check;
        ALTER TABLE system.notebook_session_authorizations
            RENAME CONSTRAINT notebook_catalog_authorizations_audience_check
            TO notebook_session_authorizations_audience_check;
        ALTER TABLE system.notebook_session_authorizations
            RENAME CONSTRAINT notebook_catalog_authorizations_check
            TO notebook_session_authorizations_check;
        ALTER TABLE system.notebook_session_authorizations
            RENAME CONSTRAINT notebook_catalog_authorizations_check1
            TO notebook_session_authorizations_check1;
        ALTER TABLE system.notebook_session_authorizations
            RENAME CONSTRAINT notebook_catalog_authorizations_check2
            TO notebook_session_authorizations_check2;
        ALTER TABLE system.notebook_session_authorizations
            RENAME CONSTRAINT notebook_catalog_authorizations_check3
            TO notebook_session_authorizations_check3;
        ALTER TABLE system.notebook_session_authorizations
            RENAME CONSTRAINT notebook_catalog_authorizations_actor_principal_id_fkey
            TO notebook_session_authorizations_actor_principal_id_fkey;
        ALTER TABLE system.notebook_session_authorizations
            RENAME CONSTRAINT notebook_catalog_authorizations_tenant_id_fkey
            TO notebook_session_authorizations_tenant_id_fkey;
        ALTER TABLE system.notebook_session_authorizations
            RENAME CONSTRAINT notebook_catalog_authorizations_tenant_membership_id_fkey
            TO notebook_session_authorizations_tenant_membership_id_fkey;
        ALTER TABLE system.notebook_session_authorizations
            RENAME CONSTRAINT notebook_catalog_authorizations_token_family_id_fkey
            TO notebook_session_authorizations_token_family_id_fkey;

        ALTER INDEX system.idx_notebook_catalog_authorizations_actor_active
            RENAME TO idx_notebook_session_authorizations_actor_active;
        ALTER INDEX system.idx_notebook_catalog_authorizations_membership_active
            RENAME TO idx_notebook_session_authorizations_membership_active;
        ALTER INDEX system.idx_notebook_catalog_authorizations_family_active
            RENAME TO idx_notebook_session_authorizations_family_active;
        ALTER INDEX system.idx_notebook_catalog_authorizations_expiry_active
            RENAME TO idx_notebook_session_authorizations_expiry_active;

        DROP FUNCTION IF EXISTS system.validate_notebook_catalog_authorization();
        DROP FUNCTION IF EXISTS system.validate_notebook_catalog_authorization_update();
    END IF;

    IF to_regclass('system.notebook_session_authorizations') IS NULL THEN
        CREATE TABLE system.notebook_session_authorizations (
            id uuid PRIMARY KEY,
            session_id uuid NOT NULL UNIQUE,
            task_id bigint NOT NULL CHECK (task_id > 0),
            actor_principal_id bigint NOT NULL REFERENCES system.principals(id),
            tenant_id bigint NOT NULL REFERENCES system.tenants(id),
            tenant_membership_id bigint NOT NULL REFERENCES system.tenant_memberships(id),
            token_family_id bigint NOT NULL REFERENCES system.refresh_token_families(id),
            issued_authorization_version bigint NOT NULL CHECK (issued_authorization_version > 0),
            audience text NOT NULL CHECK (audience = 'develop'),
            operations text[] NOT NULL CHECK (
                operations = ARRAY['catalog.list_children', 'execution_engine_access.derive']::text[]
            ),
            expires_at timestamptz NOT NULL,
            revoked_at timestamptz,
            revoked_reason text,
            created_at timestamptz NOT NULL DEFAULT now(),
            CHECK (expires_at > created_at),
            CHECK (expires_at <= created_at + interval '1 hour'),
            CHECK (
                (revoked_at IS NULL AND revoked_reason IS NULL)
                OR (revoked_at IS NOT NULL AND revoked_reason IS NOT NULL AND btrim(revoked_reason) <> '')
            ),
            CHECK (revoked_at IS NULL OR revoked_at >= created_at)
        );
    END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS idx_notebook_session_authorizations_actor_active
    ON system.notebook_session_authorizations (actor_principal_id, tenant_id, expires_at)
    WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_notebook_session_authorizations_membership_active
    ON system.notebook_session_authorizations (tenant_membership_id, expires_at)
    WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_notebook_session_authorizations_family_active
    ON system.notebook_session_authorizations (token_family_id, expires_at)
    WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_notebook_session_authorizations_expiry_active
    ON system.notebook_session_authorizations (expires_at, id)
    WHERE revoked_at IS NULL;

CREATE OR REPLACE FUNCTION system.validate_notebook_session_authorization()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM system.refresh_token_families family
        JOIN system.principals principal
          ON principal.id = family.principal_id
        JOIN system.tenant_memberships membership
          ON membership.id = family.tenant_membership_id
         AND membership.principal_id = family.principal_id
        JOIN system.tenants tenant
          ON tenant.id = membership.tenant_id
        WHERE family.id = NEW.token_family_id
          AND family.principal_id = NEW.actor_principal_id
          AND family.context_type = 'tenant'
          AND family.tenant_membership_id = NEW.tenant_membership_id
          AND family.revoked_at IS NULL
          AND family.expires_at > NEW.created_at
          AND principal.principal_type = 'user'
          AND principal.status = 'active'
          AND principal.authorization_version = NEW.issued_authorization_version
          AND membership.id = NEW.tenant_membership_id
          AND membership.tenant_id = NEW.tenant_id
          AND membership.status = 'active'
          AND (membership.expires_at IS NULL OR membership.expires_at > NEW.created_at)
          AND tenant.status = 'active'
        FOR KEY SHARE OF family, principal, membership, tenant
    ) THEN
        RAISE EXCEPTION 'notebook session authorization requires an active tenant user token family'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.expires_at > (
        SELECT family.expires_at
        FROM system.refresh_token_families family
        WHERE family.id = NEW.token_family_id
    ) THEN
        RAISE EXCEPTION 'notebook session authorization expiry exceeds its token family'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION system.validate_notebook_session_authorization_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.session_id IS DISTINCT FROM OLD.session_id
       OR NEW.task_id IS DISTINCT FROM OLD.task_id
       OR NEW.actor_principal_id IS DISTINCT FROM OLD.actor_principal_id
       OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.tenant_membership_id IS DISTINCT FROM OLD.tenant_membership_id
       OR NEW.token_family_id IS DISTINCT FROM OLD.token_family_id
       OR NEW.issued_authorization_version IS DISTINCT FROM OLD.issued_authorization_version
       OR NEW.audience IS DISTINCT FROM OLD.audience
       OR NEW.operations IS DISTINCT FROM OLD.operations
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'notebook session authorization identity and boundary are immutable'
            USING ERRCODE = '23514';
    END IF;
    IF OLD.revoked_at IS NOT NULL
       AND (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
            OR NEW.revoked_reason IS DISTINCT FROM OLD.revoked_reason) THEN
        RAISE EXCEPTION 'notebook session authorization revocation is immutable'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_notebook_session_authorizations_validate
    ON system.notebook_session_authorizations;
CREATE TRIGGER trg_notebook_session_authorizations_validate
BEFORE INSERT ON system.notebook_session_authorizations
FOR EACH ROW EXECUTE FUNCTION system.validate_notebook_session_authorization();
DROP TRIGGER IF EXISTS trg_notebook_session_authorizations_validate_update
    ON system.notebook_session_authorizations;
CREATE TRIGGER trg_notebook_session_authorizations_validate_update
BEFORE UPDATE ON system.notebook_session_authorizations
FOR EACH ROW EXECUTE FUNCTION system.validate_notebook_session_authorization_update();
DROP TRIGGER IF EXISTS trg_notebook_session_authorizations_prevent_delete
    ON system.notebook_session_authorizations;
CREATE TRIGGER trg_notebook_session_authorizations_prevent_delete
BEFORE DELETE ON system.notebook_session_authorizations
FOR EACH ROW EXECUTE FUNCTION system.prevent_token_history_delete();

ALTER TABLE system.execution_authorizations
    ADD COLUMN IF NOT EXISTS source_notebook_session_authorization_id uuid
        REFERENCES system.notebook_session_authorizations(id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'system.execution_authorizations'::regclass
          AND conname = 'execution_authorizations_notebook_session_source_check'
    ) THEN
        ALTER TABLE system.execution_authorizations
            ADD CONSTRAINT execution_authorizations_notebook_session_source_check CHECK (
                source_notebook_session_authorization_id IS NULL
                OR (
                    source_type = 'user'
                    AND audience = 'develop'
                    AND effects = ARRAY['read']::text[]
                    AND cardinality(engine_ids) = 1
                )
            );
    END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS idx_execution_authorizations_notebook_session_active
    ON system.execution_authorizations (source_notebook_session_authorization_id, expires_at)
    WHERE source_notebook_session_authorization_id IS NOT NULL AND revoked_at IS NULL;

CREATE OR REPLACE FUNCTION system.validate_execution_authorization()
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

CREATE OR REPLACE FUNCTION system.validate_execution_authorization_update()
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

CREATE OR REPLACE FUNCTION system.revoke_notebook_session_execution_authorizations()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL THEN
        UPDATE system.execution_authorizations
        SET revoked_at = NEW.revoked_at,
            revoked_reason = 'notebook_session_revoked'
        WHERE source_notebook_session_authorization_id = NEW.id
          AND revoked_at IS NULL;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_notebook_session_authorizations_revoke_executions
    ON system.notebook_session_authorizations;
CREATE TRIGGER trg_notebook_session_authorizations_revoke_executions
AFTER UPDATE OF revoked_at ON system.notebook_session_authorizations
FOR EACH ROW EXECUTE FUNCTION system.revoke_notebook_session_execution_authorizations();

CREATE OR REPLACE FUNCTION system.revoke_token_family_derivatives()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL THEN
        UPDATE system.refresh_tokens
        SET revoked_at = NEW.revoked_at
        WHERE family_id = NEW.id AND revoked_at IS NULL;

        UPDATE system.access_tokens
        SET revoked_at = NEW.revoked_at
        WHERE family_id = NEW.id AND revoked_at IS NULL;

        UPDATE system.resource_access_tickets
        SET revoked_at = NEW.revoked_at
        WHERE family_id = NEW.id AND revoked_at IS NULL;

        UPDATE system.notebook_session_authorizations
        SET revoked_at = NEW.revoked_at,
            revoked_reason = 'token_family_revoked'
        WHERE token_family_id = NEW.id AND revoked_at IS NULL;
    END IF;
    RETURN NEW;
END;
$$;

DO $$
BEGIN
    DELETE FROM system.role_permissions
    WHERE permission_id IN (
        SELECT id FROM system.permissions
        WHERE permission_key = 'system.notebook_catalog_authorization.execute'
    );

    UPDATE system.permissions
    SET status = 'disabled', updated_at = now()
    WHERE permission_key = 'system.notebook_catalog_authorization.execute'
      AND status = 'active';

    INSERT INTO system.permissions (
        permission_key, owner_module, action, risk_level, delegable,
        allowed_scope_types, tenant_customizable, name_i18n_key,
        description_i18n_key, status
    )
    SELECT
        'system.notebook_session_authorization.execute', 'system', 'execute', 'low', false,
        ARRAY['tenant']::text[], false,
        'permissions.system.notebook_session_authorization.execute.name',
        'permissions.system.notebook_session_authorization.execute.description', 'active'
    WHERE NOT EXISTS (
        SELECT 1 FROM system.permissions
        WHERE permission_key = 'system.notebook_session_authorization.execute'
    );

    IF to_regclass('system.notebook_catalog_authorizations') IS NOT NULL
       OR to_regprocedure('system.validate_notebook_catalog_authorization()') IS NOT NULL
       OR to_regprocedure('system.validate_notebook_catalog_authorization_update()') IS NOT NULL THEN
        RAISE EXCEPTION 'legacy notebook catalog authorization schema still exists';
    END IF;

    IF EXISTS (
        SELECT 1 FROM system.permissions
        WHERE permission_key = 'system.notebook_catalog_authorization.execute'
          AND status <> 'disabled'
    ) THEN
        RAISE EXCEPTION 'legacy notebook catalog authorization permission is not retired';
    END IF;
END;
$$;

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key = 'system.notebook_session_authorization.execute'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.develop_runtime'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM system.role_permissions existing
      WHERE existing.role_id = role.id
        AND existing.permission_id = permission.id
  );

COMMIT;
