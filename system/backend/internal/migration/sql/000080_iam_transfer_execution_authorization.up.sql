BEGIN;

ALTER TABLE system.execution_authorizations
    DROP CONSTRAINT execution_authorizations_audience_check;

ALTER TABLE system.execution_authorizations
    ADD CONSTRAINT execution_authorizations_audience_check
        CHECK (audience IN ('develop', 'duckdb', 'model', 'quality', 'service', 'transfer'));

ALTER TABLE system.execution_authorizations
    DROP CONSTRAINT execution_authorizations_audience_execution_id_key,
    ADD COLUMN source_execution_attempt integer,
    ADD COLUMN source_execution_lease_token uuid,
    ADD CONSTRAINT execution_authorizations_execution_attempt_check CHECK (
        (source_execution_attempt IS NULL AND source_execution_lease_token IS NULL)
        OR (
            source_execution_attempt > 0
            AND source_execution_lease_token IS NOT NULL
            AND source_type = 'user'
            AND audience = 'transfer'
        )
    );

CREATE UNIQUE INDEX uq_execution_authorizations_static_execution
    ON system.execution_authorizations (audience, execution_id)
    WHERE source_execution_attempt IS NULL;

CREATE UNIQUE INDEX uq_execution_authorizations_execution_attempt
    ON system.execution_authorizations (audience, execution_id, source_execution_attempt)
    WHERE source_execution_attempt IS NOT NULL;

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
       OR NEW.source_execution_attempt IS DISTINCT FROM OLD.source_execution_attempt
       OR NEW.source_execution_lease_token IS DISTINCT FROM OLD.source_execution_lease_token
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

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'system.execution_authorization.execute'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.transfer_runtime'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
