CREATE TRIGGER IF NOT EXISTS oauth_pkce_validate_update
BEFORE UPDATE ON oauth_pkce_sessions FOR EACH ROW
BEGIN
    IF NEW.id<>OLD.id OR NEW.authorization_request_id<>OLD.authorization_request_id
        OR NEW.code_challenge<>OLD.code_challenge OR NEW.code_challenge_method<>OLD.code_challenge_method
        OR NEW.expires_at<>OLD.expires_at OR NEW.created_at<>OLD.created_at
        OR (OLD.authorization_code_hash IS NOT NULL AND NOT (NEW.authorization_code_hash <=> OLD.authorization_code_hash))
        OR (OLD.verified_at IS NOT NULL AND NOT (NEW.verified_at <=> OLD.verified_at))
        OR (OLD.consumed_at IS NOT NULL AND NOT (NEW.consumed_at <=> OLD.consumed_at))
        OR (NEW.authorization_code_hash IS NOT NULL AND NOT EXISTS (
            SELECT 1 FROM oauth_authorization_requests WHERE id=NEW.authorization_request_id AND status='approved' FOR SHARE)) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='PKCE protocol facts and terminal timestamps are immutable';
    END IF;
END
