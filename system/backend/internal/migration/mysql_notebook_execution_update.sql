CREATE TRIGGER IF NOT EXISTS notebook_validate_execution_seal
BEFORE UPDATE ON execution_authorizations FOR EACH ROW
BEGIN
    IF NOT (NEW.source_notebook_session_authorization_id <=> OLD.source_notebook_session_authorization_id) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='Notebook execution provenance is immutable';
    END IF;
    IF NEW.source_notebook_session_authorization_id IS NOT NULL AND OLD.sealed_at IS NULL AND NEW.sealed_at IS NOT NULL
       AND (SELECT COUNT(*)=1 AND SUM(effects=JSON_ARRAY('read'))=1
            FROM execution_authorization_engine_accesses WHERE authorization_id=NEW.id FOR SHARE)<>1 THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='Notebook execution requires one read-only engine';
    END IF;
END
