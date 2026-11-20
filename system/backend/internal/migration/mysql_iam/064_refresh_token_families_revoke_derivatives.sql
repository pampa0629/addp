CREATE TRIGGER IF NOT EXISTS trg_refresh_token_families_revoke_derivatives
AFTER UPDATE ON refresh_token_families FOR EACH ROW
BEGIN
    IF OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL THEN
        UPDATE refresh_tokens SET revoked_at = NEW.revoked_at WHERE family_id = NEW.id AND revoked_at IS NULL;
        UPDATE access_tokens SET revoked_at = NEW.revoked_at WHERE family_id = NEW.id AND revoked_at IS NULL;
        UPDATE resource_access_tickets SET revoked_at = NEW.revoked_at WHERE family_id = NEW.id AND revoked_at IS NULL;
    END IF;
END
