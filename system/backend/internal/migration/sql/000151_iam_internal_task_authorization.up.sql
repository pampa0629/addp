BEGIN;

ALTER TABLE system.execution_authorizations
    ADD COLUMN internal_task jsonb,
    DROP CONSTRAINT execution_authorizations_audience_check,
    ADD CONSTRAINT execution_authorizations_audience_check CHECK (audience IN ('model','quality','develop','transfer','service','duckdb','ontology')),
    ADD CONSTRAINT execution_authorizations_internal_task_check CHECK (
        (internal_task IS NULL AND audience <> 'ontology')
        OR ((
            audience = 'ontology' AND source_type = 'user'
            AND source_notebook_session_authorization_id IS NULL
            AND source_execution_attempt IS NULL AND source_execution_lease_token IS NULL
            AND jsonb_typeof(internal_task) = 'object'
            AND internal_task ?& ARRAY['task_type','resource_id','revision','digest','generation']
            AND internal_task - ARRAY['task_type','resource_id','revision','digest','generation'] = '{}'::jsonb
            AND internal_task->>'task_type' = 'semantic_projection'
            AND jsonb_typeof(internal_task->'task_type') = 'string'
            AND jsonb_typeof(internal_task->'resource_id') = 'string'
            AND jsonb_typeof(internal_task->'revision') = 'string'
            AND jsonb_typeof(internal_task->'digest') = 'string'
            AND jsonb_typeof(internal_task->'generation') = 'string'
            AND internal_task->>'resource_id' ~ '^[a-z][a-z0-9_]{0,63}$'
            AND internal_task->>'revision' ~ '^[1-9][0-9]{0,18}$'
            AND (internal_task->>'revision')::numeric <= 9223372036854775807
            AND internal_task->>'digest' ~ '^[0-9a-f]{64}$'
            AND internal_task->>'generation' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
            AND internal_task->>'generation' <> '00000000-0000-0000-0000-000000000000'
        ) IS TRUE)
    );

CREATE OR REPLACE FUNCTION system.validate_execution_authorization_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.internal_task IS DISTINCT FROM OLD.internal_task
       OR NEW.actor_principal_id IS DISTINCT FROM OLD.actor_principal_id
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
           OR (NEW.internal_task IS NULL AND NOT EXISTS (
                SELECT 1
                FROM system.execution_authorization_engine_accesses access
                WHERE access.authorization_id = NEW.id
           )) THEN
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

CREATE FUNCTION system.validate_internal_task_execution_authorization()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.internal_task IS NULL THEN
        RETURN NEW;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM common.task_executions e
        WHERE e.execution_id = NEW.execution_id::text AND e.tenant_id = NEW.tenant_id
          AND e.module = 'ontology' AND e.source = 'ontology'
          AND e.task_type = NEW.internal_task->>'task_type' AND e.execution_boundary = 'bounded'
          AND e.status = 'pending' AND e.execution_authorization_id IS NULL
          AND e.actor_principal_id = NEW.actor_principal_id
          AND e.actor_tenant_membership_id = NEW.tenant_membership_id
          AND e.issued_authorization_version = NEW.issued_authorization_version
          AND e.execution_config = jsonb_build_object(
            'ontology_id', NEW.internal_task->>'resource_id', 'revision', NEW.internal_task->>'revision',
            'digest', NEW.internal_task->>'digest', 'generation', NEW.internal_task->>'generation')
        FOR UPDATE OF e
    ) THEN
        RAISE EXCEPTION 'internal authorization requires the exact pending owner execution' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_internal_task_execution_authorization
BEFORE INSERT ON system.execution_authorizations
FOR EACH ROW EXECUTE FUNCTION system.validate_internal_task_execution_authorization();

CREATE FUNCTION system.reject_internal_task_engine_access()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM system.execution_authorizations WHERE id = NEW.authorization_id AND internal_task IS NOT NULL) THEN
        RAISE EXCEPTION 'internal task authorization cannot grant engine access' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_internal_task_engine_access
BEFORE INSERT ON system.execution_authorization_engine_accesses
FOR EACH ROW EXECUTE FUNCTION system.reject_internal_task_engine_access();

COMMIT;
