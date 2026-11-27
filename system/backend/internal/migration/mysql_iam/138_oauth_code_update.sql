CREATE TRIGGER IF NOT EXISTS oauth_code_validate_update
BEFORE UPDATE ON oauth_authorization_codes FOR EACH ROW
BEGIN
    IF NEW.id<>OLD.id OR NEW.code_hash<>OLD.code_hash OR NEW.authorization_request_id<>OLD.authorization_request_id
        OR NEW.expires_at<>OLD.expires_at OR NEW.created_at<>OLD.created_at
        OR OLD.invalidated_at IS NOT NULL OR NEW.invalidated_at IS NULL THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='OAuth code facts are immutable and invalidation is one-way';
    END IF;
END
