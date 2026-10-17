-- No historical owner is inferred. A scheduled definition must be saved or
-- resumed by a current authorized User before it can be triggered.
ALTER TABLE transfer.transfer_tasks
 ADD COLUMN IF NOT EXISTS authorization_subject_id BIGINT,
 ADD COLUMN IF NOT EXISTS authorization_definition_hash VARCHAR(64),
 ADD COLUMN IF NOT EXISTS authorization_principal_id BIGINT,
 ADD COLUMN IF NOT EXISTS authorization_membership_id BIGINT,
 ADD COLUMN IF NOT EXISTS authorization_version BIGINT,
 ADD COLUMN IF NOT EXISTS authorized_at TIMESTAMPTZ;
