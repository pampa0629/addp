BEGIN;

ALTER TABLE system.execution_authorizations
    DROP CONSTRAINT execution_authorizations_execution_attempt_check,
    ADD CONSTRAINT execution_authorizations_execution_attempt_check CHECK (
        (source_execution_attempt IS NULL AND source_execution_lease_token IS NULL)
        OR (
            source_execution_attempt > 0
            AND source_execution_lease_token IS NOT NULL
            AND source_type = 'user'
        )
    );

COMMIT;
