CREATE TRIGGER IF NOT EXISTS execution_authorization_prevent_delete
BEFORE DELETE ON execution_authorizations FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='execution authorization history cannot be deleted'
