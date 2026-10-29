-- Preserve the caller's selected scope separately from discovery's frozen result.
-- The new queue has not been released. Never invent submission intent for rows
-- from a partially applied development schema: remove those records first.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM system.cleanup_requests) THEN
  RAISE EXCEPTION 'cleanup request scope migration requires empty development records';
 END IF;
END $$;
ALTER TABLE system.cleanup_requests ADD COLUMN requested_scope text NOT NULL
 CHECK (octet_length(requested_scope) <= 512 AND jsonb_typeof(requested_scope::jsonb)='array');
