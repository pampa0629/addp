CREATE TRIGGER IF NOT EXISTS trg_access_tokens_before_insert
BEFORE INSERT ON access_tokens FOR EACH ROW
BEGIN
    IF NEW.revoked_at IS NOT NULL OR NOT EXISTS (
        SELECT 1 FROM refresh_token_families
         WHERE id = NEW.family_id AND revoked_at IS NULL AND expires_at > CURRENT_TIMESTAMP(6)
           AND NEW.expires_at <= expires_at
         FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'access token requires an active family and bounded expiry';
    END IF;
END
