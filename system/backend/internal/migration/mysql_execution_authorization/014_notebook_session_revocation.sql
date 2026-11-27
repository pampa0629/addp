CREATE TRIGGER IF NOT EXISTS notebook_revoke_session_executions
AFTER UPDATE ON notebook_session_authorizations FOR EACH ROW
BEGIN
    IF OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL THEN
        UPDATE execution_authorizations SET revoked_at=NEW.revoked_at, revoked_reason='notebook_session_revoked'
        WHERE source_notebook_session_authorization_id=NEW.id AND revoked_at IS NULL;
    END IF;
END
