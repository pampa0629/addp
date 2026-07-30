ALTER TABLE orchestrator.orchestrations
    ADD COLUMN IF NOT EXISTS authorization_ref uuid,
    ADD COLUMN IF NOT EXISTS authorization_subject_id bigint,
    ADD COLUMN IF NOT EXISTS authorization_definition_hash text,
    ADD COLUMN IF NOT EXISTS authorization_principal_id bigint,
    ADD COLUMN IF NOT EXISTS authorization_membership_id bigint,
    ADD COLUMN IF NOT EXISTS authorization_version bigint,
    ADD COLUMN IF NOT EXISTS authorized_at timestamptz;

UPDATE orchestrator.orchestrations
SET enabled = false,
    next_run_at = NULL
WHERE authorization_subject_id IS NULL;

ALTER TABLE orchestrator.orchestrations
    ADD CONSTRAINT orchestrations_authorization_subject_complete CHECK (
        (authorization_ref IS NULL
         AND authorization_subject_id IS NULL
         AND authorization_definition_hash IS NULL
         AND authorization_principal_id IS NULL
         AND authorization_membership_id IS NULL
         AND authorization_version IS NULL
         AND authorized_at IS NULL)
        OR
        (authorization_ref IS NOT NULL
         AND authorization_subject_id > 0
         AND authorization_definition_hash ~ '^[0-9a-f]{64}$'
         AND authorization_principal_id > 0
         AND authorization_membership_id > 0
         AND authorization_version > 0
         AND authorized_at IS NOT NULL)
    ),
    ADD CONSTRAINT orchestrations_enabled_authorization_subject CHECK (
        enabled = false OR authorization_subject_id IS NOT NULL
    );

CREATE UNIQUE INDEX IF NOT EXISTS uq_orchestrations_authorization_ref
    ON orchestrator.orchestrations (authorization_ref)
    WHERE authorization_ref IS NOT NULL;
