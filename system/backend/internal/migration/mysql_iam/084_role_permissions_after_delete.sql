CREATE TRIGGER IF NOT EXISTS trg_role_permissions_after_delete
AFTER DELETE ON role_permissions FOR EACH ROW
CALL bump_role_holders(OLD.role_id)
