-- Only System owns request acceptance and the immutable coordinator cutoff.
-- No old Redis record is imported and no historic execution is rewritten.
CREATE TABLE system.cleanup_requests (
    request_id uuid PRIMARY KEY,
    tenant_id bigint NOT NULL CHECK (tenant_id > 0),
    requested_by bigint NOT NULL CHECK (requested_by > 0),
    parent_execution_id uuid NOT NULL UNIQUE,
    intent_digest text NOT NULL CHECK (intent_digest ~ '^[a-f0-9]{64}$'),
    source_operation_id text NOT NULL DEFAULT '' CHECK (length(source_operation_id) <= 128),
    action text NOT NULL CHECK (action IN ('scan', 'execute')),
    protocol text NOT NULL CHECK (octet_length(protocol) <= 16384 AND jsonb_typeof(protocol::jsonb) = 'object'),
    engine_confirmation text NOT NULL DEFAULT '' CHECK (octet_length(engine_confirmation) <= 2048),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    cutoff text CHECK (octet_length(cutoff) <= 262144 AND jsonb_typeof(cutoff::jsonb) = 'object')
);
CREATE UNIQUE INDEX cleanup_requests_source_operation ON system.cleanup_requests(tenant_id, source_operation_id, action) WHERE source_operation_id <> '';
CREATE INDEX cleanup_requests_history ON system.cleanup_requests(tenant_id, created_at DESC, request_id DESC);

CREATE FUNCTION system.guard_cleanup_request_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (to_jsonb(NEW) - 'cutoff') IS DISTINCT FROM (to_jsonb(OLD) - 'cutoff')
       OR OLD.cutoff IS NOT NULL OR NEW.cutoff IS NULL THEN
        RAISE EXCEPTION 'cleanup acceptance and cutoff are immutable';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER cleanup_request_immutable BEFORE UPDATE ON system.cleanup_requests
FOR EACH ROW EXECUTE FUNCTION system.guard_cleanup_request_immutable();
