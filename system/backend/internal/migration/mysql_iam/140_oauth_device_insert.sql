CREATE TRIGGER IF NOT EXISTS oauth_device_validate_insert
BEFORE INSERT ON oauth_device_authorizations FOR EACH ROW
BEGIN
    IF NEW.status<>'pending' OR NOT EXISTS (
        SELECT 1 FROM oauth_clients WHERE client_id=NEW.client_id AND status='active'
        AND JSON_CONTAINS(grant_types,JSON_QUOTE('urn:ietf:params:oauth:grant-type:device_code'))
        AND JSON_CONTAINS(allowed_scopes,NEW.requested_scopes)
        AND JSON_CONTAINS(allowed_audiences,NEW.requested_audiences) FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='OAuth device code requires pending status and an active registered client scope';
    END IF;
END
