ALTER TABLE common.task_executions
    DROP CONSTRAINT IF EXISTS task_executions_execution_authorization_tuple_check;

ALTER TABLE common.task_executions
    ADD CONSTRAINT task_executions_execution_authorization_tuple_check CHECK (
        num_nonnulls(
            actor_principal_id,
            actor_tenant_membership_id,
            issued_authorization_version
        ) IN (0, 3)
        AND num_nonnulls(
            execution_authorization_id,
            authorization_effects,
            authorization_expires_at
        ) IN (0, 3)
        AND (
            execution_authorization_id IS NULL
            OR actor_principal_id IS NOT NULL
        )
        AND (actor_principal_id IS NULL OR actor_principal_id > 0)
        AND (actor_tenant_membership_id IS NULL OR actor_tenant_membership_id > 0)
        AND (issued_authorization_version IS NULL OR issued_authorization_version > 0)
        AND (execution_authorization_id IS NULL OR execution_authorization_id > 0)
        AND (authorization_effects IS NULL OR common.valid_execution_authorization_effects(authorization_effects))
        AND (authorization_expires_at IS NULL OR authorization_expires_at > created_at)
    );
