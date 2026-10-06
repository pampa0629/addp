BEGIN;

ALTER TABLE system.execution_authorizations
    ADD COLUMN source_read_scope jsonb,
    DROP CONSTRAINT execution_authorizations_audience_check,
    ADD CONSTRAINT execution_authorizations_audience_check CHECK (audience IN ('model','quality','develop','transfer','service','duckdb','ontology','manager')),
    ADD CONSTRAINT execution_authorizations_source_read_scope_check CHECK (
        (source_read_scope IS NULL AND audience <> 'manager') OR ((
            audience = 'manager' AND source_type = 'user' AND internal_task IS NULL
            AND source_notebook_session_authorization_id IS NULL
            AND source_execution_attempt IS NULL AND source_execution_lease_token IS NULL
            AND jsonb_typeof(source_read_scope) = 'object'
            AND source_read_scope ?& ARRAY['config_digest','read_set']
            AND source_read_scope - ARRAY['config_digest','read_set'] = '{}'::jsonb
            AND jsonb_typeof(source_read_scope->'config_digest') = 'string'
            AND source_read_scope->>'config_digest' ~ '^[0-9a-f]{64}$'
            AND jsonb_typeof(source_read_scope->'read_set') = 'object'
            AND (source_read_scope->'read_set') - ARRAY['paths'] = '{}'::jsonb
            AND jsonb_typeof(source_read_scope->'read_set'->'paths') = 'array'
            AND jsonb_array_length(source_read_scope->'read_set'->'paths') BETWEEN 1 AND 200
        ) IS TRUE)
    );

CREATE FUNCTION system.validate_execution_source_read_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.source_read_scope IS DISTINCT FROM OLD.source_read_scope THEN
            RAISE EXCEPTION 'execution source read scope is immutable' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.audience <> 'manager' THEN RETURN NEW; END IF;
    IF NOT EXISTS (
        SELECT 1 FROM common.task_executions e
        WHERE e.execution_id = NEW.execution_id::text AND e.tenant_id = NEW.tenant_id
          AND e.module = 'manager' AND e.source = 'manager' AND e.task_type = 'data_profiling'
          AND e.execution_boundary = 'bounded' AND e.trigger_type = 'manual'
          AND e.source_task_id IS NULL AND e.parent_execution_id IS NULL
          AND e.status = 'pending' AND e.attempt = 0
          AND e.execution_authorization_id IS NULL AND e.authorization_expires_at IS NULL
          AND e.actor_principal_id = NEW.actor_principal_id
          AND e.actor_tenant_membership_id = NEW.tenant_membership_id
          AND e.issued_authorization_version = NEW.issued_authorization_version
          AND e.execution_config->>'config_version' = 'data-profile-config/v6'
          AND e.execution_config->'read_set' = NEW.source_read_scope->'read_set'
        FOR SHARE OF e
    ) THEN
        RAISE EXCEPTION 'source scope requires the exact pending Manager execution' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_execution_source_read_scope BEFORE INSERT OR UPDATE ON system.execution_authorizations
    FOR EACH ROW EXECUTE FUNCTION system.validate_execution_source_read_scope();

CREATE FUNCTION system.validate_manager_profile_engine_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE auth system.execution_authorizations;
BEGIN
    SELECT * INTO auth FROM system.execution_authorizations WHERE id = NEW.authorization_id;
    IF auth.audience = 'manager' AND (
        NEW.effects IS DISTINCT FROM ARRAY['read']::text[] OR
        NEW.engine_id IS DISTINCT FROM (auth.source_read_scope->'read_set'->'paths'->0->>'engine_id')::bigint
    ) THEN
        RAISE EXCEPTION 'Manager profile cannot grant other engines or effects' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_manager_profile_engine_scope BEFORE INSERT ON system.execution_authorization_engine_accesses
    FOR EACH ROW EXECUTE FUNCTION system.validate_manager_profile_engine_scope();

-- Only machine execution consumption. No User role, Assignment, source Grant,
-- default source access or system.engine.read permission is introduced.
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
SELECT role.id,permission.id,'product' FROM system.roles role
JOIN system.permissions permission ON permission.permission_key='system.execution_authorization.execute'
WHERE role.tenant_id IS NULL AND role.role_key='tenant.manager_runtime'
ON CONFLICT (role_id,permission_id) DO NOTHING;
CREATE TEMP TABLE manager_execution_affected ON COMMIT DROP AS
SELECT DISTINCT a.principal_id FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
WHERE a.status='active' AND r.tenant_id IS NULL AND r.role_key='tenant.manager_runtime';
-- The existing role_permissions trigger already advances the affected
-- authorization versions. Do not count this same catalog change twice.
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
FROM manager_execution_affected a WHERE f.principal_id=a.principal_id AND f.revoked_at IS NULL;

COMMIT;
