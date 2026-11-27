CREATE TRIGGER IF NOT EXISTS execution_internal_task_reject_engine_access
BEFORE INSERT ON execution_authorization_engine_accesses FOR EACH ROW
BEGIN
    IF EXISTS (SELECT 1 FROM execution_authorizations
               WHERE id=NEW.authorization_id AND internal_task IS NOT NULL FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='internal task authorization cannot grant engine access';
    END IF;
END
