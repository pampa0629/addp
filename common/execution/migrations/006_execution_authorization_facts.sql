ALTER TABLE common.task_executions
    ADD COLUMN IF NOT EXISTS actor_principal_id BIGINT,
    ADD COLUMN IF NOT EXISTS actor_tenant_membership_id BIGINT,
    ADD COLUMN IF NOT EXISTS issued_authorization_version BIGINT,
    ADD COLUMN IF NOT EXISTS execution_authorization_id BIGINT,
    ADD COLUMN IF NOT EXISTS authorization_effects TEXT[],
    ADD COLUMN IF NOT EXISTS authorization_expires_at TIMESTAMPTZ;

CREATE FUNCTION common.valid_execution_authorization_effects(values_to_check TEXT[])
RETURNS BOOLEAN
LANGUAGE SQL
IMMUTABLE
AS $$
    SELECT values_to_check IS NOT NULL
       AND cardinality(values_to_check) > 0
       AND NOT EXISTS (
           SELECT 1
           FROM unnest(values_to_check) AS effect
           WHERE effect IS NULL
              OR effect NOT IN ('read', 'write', 'ddl', 'external_effect')
       )
       AND cardinality(values_to_check) = (
           SELECT count(DISTINCT effect)::INTEGER
           FROM unnest(values_to_check) AS effect
       );
$$;

ALTER TABLE common.task_executions
    ADD CONSTRAINT task_executions_execution_authorization_tuple_check CHECK (
        num_nonnulls(
            actor_principal_id,
            actor_tenant_membership_id,
            issued_authorization_version,
            execution_authorization_id,
            authorization_effects,
            authorization_expires_at
        ) IN (0, 6)
        AND (actor_principal_id IS NULL OR actor_principal_id > 0)
        AND (actor_tenant_membership_id IS NULL OR actor_tenant_membership_id > 0)
        AND (issued_authorization_version IS NULL OR issued_authorization_version > 0)
        AND (execution_authorization_id IS NULL OR execution_authorization_id > 0)
        AND (authorization_effects IS NULL OR common.valid_execution_authorization_effects(authorization_effects))
        AND (authorization_expires_at IS NULL OR authorization_expires_at > created_at)
    );

CREATE UNIQUE INDEX uq_task_executions_execution_authorization
    ON common.task_executions (execution_authorization_id)
    WHERE execution_authorization_id IS NOT NULL;
