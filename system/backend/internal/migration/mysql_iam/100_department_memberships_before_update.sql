CREATE TRIGGER IF NOT EXISTS trg_department_memberships_before_update
BEFORE UPDATE ON department_memberships FOR EACH ROW
BEGIN
    IF NEW.tenant_id<>OLD.tenant_id OR NEW.department_id<>OLD.department_id OR NEW.tenant_membership_id<>OLD.tenant_membership_id THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='department membership identity cannot change';
    END IF;
    CALL validate_department_membership(NEW.tenant_id,NEW.department_id,NEW.tenant_membership_id,NEW.status);
    SET NEW.updated_at=CURRENT_TIMESTAMP(6);
END
