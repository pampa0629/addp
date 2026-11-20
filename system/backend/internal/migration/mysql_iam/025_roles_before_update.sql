CREATE TRIGGER IF NOT EXISTS trg_roles_before_update
BEFORE UPDATE ON roles FOR EACH ROW
BEGIN
    IF NOT (NEW.tenant_id <=> OLD.tenant_id) OR NEW.role_key <> OLD.role_key
       OR NEW.role_type <> OLD.role_type
       OR NOT (NEW.created_by_principal_id <=> OLD.created_by_principal_id)
       OR NEW.created_at <> OLD.created_at THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'role identity cannot change';
    END IF;
    SET NEW.updated_at = CURRENT_TIMESTAMP(6);
END
