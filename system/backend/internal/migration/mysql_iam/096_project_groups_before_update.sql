CREATE TRIGGER IF NOT EXISTS trg_project_groups_before_update
BEFORE UPDATE ON project_groups FOR EACH ROW
BEGIN
    IF NEW.tenant_id<>OLD.tenant_id THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='project group tenant cannot change';
    END IF;
    SET NEW.updated_at=CURRENT_TIMESTAMP(6);
END
