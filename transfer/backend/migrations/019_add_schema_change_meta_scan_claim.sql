ALTER TABLE transfer.schema_change_requests
    ADD COLUMN IF NOT EXISTS metadata_scan_claim_token VARCHAR(36),
    ADD COLUMN IF NOT EXISTS metadata_scan_lease_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS metadata_scan_attempt BIGINT;

-- A pre-fencing running claim has no owner token and cannot complete safely.
-- Return it to pending so the next repeated approval can claim it once.
UPDATE transfer.schema_change_requests
SET metadata_scan_status = CASE
        WHEN metadata_scan_status = 'running' THEN 'pending'
        ELSE COALESCE(metadata_scan_status, '')
    END,
    metadata_scan_claim_token = '',
    metadata_scan_lease_until = NULL,
    metadata_scan_attempt = COALESCE(metadata_scan_attempt, 0),
    metadata_scan_execution_id = COALESCE(metadata_scan_execution_id, ''),
    metadata_scan_error = COALESCE(metadata_scan_error, '');

ALTER TABLE transfer.schema_change_requests
    ALTER COLUMN metadata_scan_status SET DEFAULT '',
    ALTER COLUMN metadata_scan_status SET NOT NULL,
    ALTER COLUMN metadata_scan_claim_token SET DEFAULT '',
    ALTER COLUMN metadata_scan_claim_token SET NOT NULL,
    ALTER COLUMN metadata_scan_attempt SET DEFAULT 0,
    ALTER COLUMN metadata_scan_attempt SET NOT NULL,
    ALTER COLUMN metadata_scan_execution_id SET DEFAULT '',
    ALTER COLUMN metadata_scan_execution_id SET NOT NULL,
    ALTER COLUMN metadata_scan_error SET DEFAULT '',
    ALTER COLUMN metadata_scan_error SET NOT NULL;

ALTER TABLE transfer.schema_change_requests
    DROP CONSTRAINT IF EXISTS schema_change_requests_metadata_scan_status_check,
    DROP CONSTRAINT IF EXISTS schema_change_requests_metadata_scan_attempt_check,
    DROP CONSTRAINT IF EXISTS chk_transfer_schema_change_meta_scan_status,
    DROP CONSTRAINT IF EXISTS chk_transfer_schema_change_meta_scan_attempt,
    DROP CONSTRAINT IF EXISTS chk_transfer_schema_change_meta_scan_claim;

ALTER TABLE transfer.schema_change_requests
    ADD CONSTRAINT chk_transfer_schema_change_meta_scan_status
        CHECK (metadata_scan_status IN ('', 'pending', 'running', 'success', 'failed')),
    ADD CONSTRAINT chk_transfer_schema_change_meta_scan_attempt
        CHECK (metadata_scan_attempt >= 0),
    ADD CONSTRAINT chk_transfer_schema_change_meta_scan_claim
        CHECK (
            (metadata_scan_status = 'running' AND metadata_scan_claim_token <> '' AND metadata_scan_lease_until IS NOT NULL)
            OR
            (metadata_scan_status <> 'running' AND metadata_scan_claim_token = '' AND metadata_scan_lease_until IS NULL)
        );
