CREATE TRIGGER IF NOT EXISTS oauth_code_validate_insert
BEFORE INSERT ON oauth_authorization_codes FOR EACH ROW
BEGIN
    IF NEW.invalidated_at IS NOT NULL OR NOT EXISTS (SELECT 1 FROM oauth_authorization_requests
        WHERE id=NEW.authorization_request_id AND status='approved' AND expires_at>=NEW.expires_at FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='OAuth code requires an approved request with bounded expiry';
    END IF;
END
