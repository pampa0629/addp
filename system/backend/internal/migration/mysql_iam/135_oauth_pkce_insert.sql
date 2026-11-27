CREATE TRIGGER IF NOT EXISTS oauth_pkce_validate_insert
BEFORE INSERT ON oauth_pkce_sessions FOR EACH ROW
BEGIN
    IF NEW.authorization_code_hash IS NOT NULL OR NEW.verified_at IS NOT NULL OR NEW.consumed_at IS NOT NULL
        OR NOT EXISTS (SELECT 1 FROM oauth_authorization_requests
            WHERE id=NEW.authorization_request_id AND status='pending' AND expires_at>=NEW.expires_at FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='PKCE requires a pending authorization request with bounded expiry';
    END IF;
END
