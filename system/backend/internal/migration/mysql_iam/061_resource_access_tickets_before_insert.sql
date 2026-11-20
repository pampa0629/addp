CREATE TRIGGER IF NOT EXISTS trg_resource_access_tickets_before_insert
BEFORE INSERT ON resource_access_tickets FOR EACH ROW
BEGIN
    IF NEW.revoked_at IS NOT NULL OR NOT EXISTS (
        SELECT 1 FROM refresh_token_families
         WHERE id = NEW.family_id AND auth_type = 'first_party'
           AND revoked_at IS NULL AND expires_at > CURRENT_TIMESTAMP(6) AND NEW.expires_at <= expires_at
         FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'resource ticket requires an active first-party family and bounded expiry';
    END IF;
END
