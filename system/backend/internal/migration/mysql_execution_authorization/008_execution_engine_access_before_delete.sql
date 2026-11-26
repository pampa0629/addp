CREATE TRIGGER IF NOT EXISTS execution_engine_access_prevent_delete
BEFORE DELETE ON execution_authorization_engine_accesses FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='execution authorization engine accesses cannot be deleted'
