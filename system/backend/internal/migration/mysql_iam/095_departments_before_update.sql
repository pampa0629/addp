CREATE TRIGGER IF NOT EXISTS trg_departments_before_update
BEFORE UPDATE ON departments FOR EACH ROW
BEGIN
    IF NEW.tenant_id<>OLD.tenant_id THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='department tenant cannot change';
    END IF;
    CALL validate_department_tree(NEW.tenant_id,NEW.id,NEW.parent_id);
    SET NEW.updated_at=CURRENT_TIMESTAMP(6);
END
