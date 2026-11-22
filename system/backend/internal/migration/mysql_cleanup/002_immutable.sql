CREATE TRIGGER IF NOT EXISTS cleanup_request_immutable
BEFORE UPDATE ON cleanup_requests FOR EACH ROW
BEGIN
    IF OLD.cutoff IS NOT NULL OR NEW.cutoff IS NULL
        OR NEW.request_id<>OLD.request_id OR NEW.tenant_id<>OLD.tenant_id
        OR NEW.requested_by<>OLD.requested_by OR NEW.parent_execution_id<>OLD.parent_execution_id
        OR NEW.intent_digest<>OLD.intent_digest OR NEW.source_operation_id<>OLD.source_operation_id
        OR NEW.action<>OLD.action OR NEW.protocol<>OLD.protocol OR NEW.engine_confirmation<>OLD.engine_confirmation
        OR NEW.created_at<>OLD.created_at OR NEW.requested_scope<>OLD.requested_scope THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='cleanup acceptance and cutoff are immutable';
    END IF;
END
