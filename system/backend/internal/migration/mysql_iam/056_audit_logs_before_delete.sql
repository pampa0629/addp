CREATE TRIGGER IF NOT EXISTS trg_audit_logs_before_delete
BEFORE DELETE ON audit_logs FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit logs are append-only'
