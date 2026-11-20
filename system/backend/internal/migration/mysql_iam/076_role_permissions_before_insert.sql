CREATE TRIGGER IF NOT EXISTS trg_role_permissions_before_insert
BEFORE INSERT ON role_permissions FOR EACH ROW
CALL validate_role_permission(NEW.role_id, NEW.permission_id, NEW.source_type, NEW.created_by_principal_id)
