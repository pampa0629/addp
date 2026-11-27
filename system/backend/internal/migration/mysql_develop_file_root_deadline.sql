CREATE TRIGGER IF NOT EXISTS develop_file_write_deadline
BEFORE UPDATE ON execution_authorizations FOR EACH ROW
BEGIN
    IF OLD.develop_file_business_deadline IS NULL AND NEW.develop_file_business_deadline IS NOT NULL
       AND JSON_CONTAINS_PATH(NEW.develop_file_scope,'one','$.targets')
       AND NOT EXISTS (SELECT 1 FROM common.task_executions WHERE execution_id=NEW.execution_id AND tenant_id=NEW.tenant_id
                       AND source='develop' AND parent_execution_id IS NULL AND trigger_type='manual' FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='Develop write deadline requires manual root';
    END IF;
END
