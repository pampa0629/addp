CREATE TRIGGER IF NOT EXISTS trg_roles_after_catalog_update
AFTER UPDATE ON roles FOR EACH ROW
CALL bump_role_holders(NEW.id)
