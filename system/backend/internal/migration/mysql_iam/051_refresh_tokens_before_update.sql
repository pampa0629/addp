CREATE TRIGGER IF NOT EXISTS trg_refresh_tokens_before_update
BEFORE UPDATE ON refresh_tokens FOR EACH ROW
BEGIN
    IF NEW.id <> OLD.id OR NEW.token_hash <> OLD.token_hash OR NEW.family_id <> OLD.family_id
       OR NEW.issued_access_token_id <> OLD.issued_access_token_id OR NOT (NEW.parent_token_id <=> OLD.parent_token_id)
       OR NEW.expires_at <> OLD.expires_at OR NEW.created_at <> OLD.created_at
       OR (OLD.used_at IS NOT NULL AND NOT (NEW.used_at <=> OLD.used_at))
       OR (OLD.reuse_detected_at IS NOT NULL AND NOT (NEW.reuse_detected_at <=> OLD.reuse_detected_at))
       OR (OLD.revoked_at IS NOT NULL AND NOT (NEW.revoked_at <=> OLD.revoked_at))
       OR (OLD.replaced_by_token_id IS NOT NULL AND NOT (NEW.replaced_by_token_id <=> OLD.replaced_by_token_id)) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'refresh token identity and terminal facts are immutable';
    END IF;
    IF NEW.replaced_by_token_id IS NOT NULL AND OLD.replaced_by_token_id IS NULL AND NOT EXISTS (
        SELECT 1 FROM refresh_tokens
         WHERE id = NEW.replaced_by_token_id AND family_id = NEW.family_id AND parent_token_id = NEW.id
         FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'refresh token replacement must match its parent and family';
    END IF;
END
