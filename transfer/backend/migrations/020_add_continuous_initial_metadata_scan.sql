ALTER TABLE transfer.transfer_tasks
    ADD COLUMN IF NOT EXISTS initial_metadata_scan_status VARCHAR(20) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS initial_metadata_scan_claim_token VARCHAR(36) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS initial_metadata_scan_lease_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS initial_metadata_scan_attempt BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS initial_metadata_scan_execution_id VARCHAR(36) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS initial_metadata_scan_error TEXT NOT NULL DEFAULT '';

-- Console 旧实现对 continuous task 固定写入 false，并非用户选择。
-- clean break 后统一启用首次目标结构扫描。
UPDATE transfer.transfer_tasks
SET auto_scan_metadata = TRUE
WHERE config #>> '{runtime,boundary}' = 'continuous';

ALTER TABLE transfer.transfer_tasks
    DROP CONSTRAINT IF EXISTS chk_transfer_initial_meta_scan_status,
    DROP CONSTRAINT IF EXISTS chk_transfer_initial_meta_scan_attempt,
    DROP CONSTRAINT IF EXISTS chk_transfer_initial_meta_scan_claim;

ALTER TABLE transfer.transfer_tasks
    ADD CONSTRAINT chk_transfer_initial_meta_scan_status
        CHECK (initial_metadata_scan_status IN ('', 'running', 'success', 'failed')),
    ADD CONSTRAINT chk_transfer_initial_meta_scan_attempt
        CHECK (initial_metadata_scan_attempt >= 0),
    ADD CONSTRAINT chk_transfer_initial_meta_scan_claim
        CHECK (
            (initial_metadata_scan_status = 'running' AND initial_metadata_scan_claim_token <> '' AND initial_metadata_scan_lease_until IS NOT NULL)
            OR
            (initial_metadata_scan_status <> 'running' AND initial_metadata_scan_claim_token = '' AND initial_metadata_scan_lease_until IS NULL)
        );
