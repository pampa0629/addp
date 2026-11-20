CREATE TRIGGER IF NOT EXISTS trg_role_permissions_after_update
AFTER UPDATE ON role_permissions FOR EACH ROW
CALL bump_role_holders(NEW.role_id)
