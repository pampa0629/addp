CREATE TRIGGER IF NOT EXISTS notebook_revoke_family_sessions
AFTER UPDATE ON refresh_token_families FOR EACH ROW
BEGIN
    IF OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL THEN
        UPDATE notebook_session_authorizations SET revoked_at=NEW.revoked_at, revoked_reason='token_family_revoked'
        WHERE token_family_id=NEW.id AND revoked_at IS NULL;
    END IF;
END
