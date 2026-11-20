CREATE TRIGGER IF NOT EXISTS trg_refresh_tokens_before_insert
BEFORE INSERT ON refresh_tokens FOR EACH ROW
BEGIN
    IF NEW.used_at IS NOT NULL OR NEW.reuse_detected_at IS NOT NULL OR NEW.revoked_at IS NOT NULL
       OR NEW.replaced_by_token_id IS NOT NULL THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'new refresh token must be current and unused';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM refresh_token_families AS family
         JOIN access_tokens AS access ON access.family_id = family.id
         WHERE family.id = NEW.family_id AND family.revoked_at IS NULL AND family.expires_at > CURRENT_TIMESTAMP(6)
           AND NEW.expires_at <= family.expires_at AND access.id = NEW.issued_access_token_id AND access.revoked_at IS NULL
         FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'refresh token requires an active family and matching access token';
    END IF;
    IF NEW.parent_token_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM refresh_tokens
         WHERE id = NEW.parent_token_id AND family_id = NEW.family_id AND used_at IS NOT NULL AND replaced_by_token_id IS NULL
         FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'refresh token parent must be used and unreplaced in the same family';
    END IF;
END
