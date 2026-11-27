CREATE TRIGGER IF NOT EXISTS notebook_session_prevent_delete
BEFORE DELETE ON notebook_session_authorizations FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='Notebook session history is immutable';
END
