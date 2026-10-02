BEGIN;
-- NULL means no diagnostic evidence was recorded; preserve historical reports.
ALTER TABLE system.module_log_source_nodes ADD COLUMN scan_issues jsonb;
ALTER TABLE system.module_log_source_nodes ADD CONSTRAINT module_log_source_scan_issues_array
  CHECK (scan_issues IS NULL OR jsonb_typeof(scan_issues) = 'array');
COMMIT;
