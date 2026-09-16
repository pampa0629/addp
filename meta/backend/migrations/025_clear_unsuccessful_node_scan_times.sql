-- Old running/failed nodes stored scan start time in scanned_at.
-- The previous successful completion time cannot be recovered from that column.
-- Invalidate only these ambiguous timestamps; preserve status, depth and content.
UPDATE meta.meta_node
SET scanned_at = NULL
WHERE scan_status IN ('running', 'failed') AND scanned_at IS NOT NULL;
