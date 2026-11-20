CREATE TRIGGER IF NOT EXISTS trg_project_group_memberships_before_update
BEFORE UPDATE ON project_group_memberships FOR EACH ROW
BEGIN
    IF NEW.tenant_id<>OLD.tenant_id OR NEW.project_group_id<>OLD.project_group_id OR NEW.tenant_membership_id<>OLD.tenant_membership_id THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='project_group membership identity cannot change';
    END IF;
    CALL validate_project_group_membership(NEW.tenant_id,NEW.project_group_id,NEW.tenant_membership_id,NEW.status);
    SET NEW.updated_at=CURRENT_TIMESTAMP(6);
END
