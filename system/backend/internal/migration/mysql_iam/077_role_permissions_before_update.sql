CREATE TRIGGER IF NOT EXISTS trg_role_permissions_before_update
BEFORE UPDATE ON role_permissions FOR EACH ROW
BEGIN
    IF NEW.role_id<>OLD.role_id OR NEW.permission_id<>OLD.permission_id OR NEW.source_type<>OLD.source_type
       OR NOT (NEW.created_by_principal_id <=> OLD.created_by_principal_id) OR NEW.created_at<>OLD.created_at THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='role permission identity cannot change';
    END IF;
    CALL validate_role_permission(NEW.role_id, NEW.permission_id, NEW.source_type, NEW.created_by_principal_id);
END
