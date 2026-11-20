CREATE TRIGGER IF NOT EXISTS trg_role_assignments_after_insert
AFTER INSERT ON role_assignments FOR EACH ROW
UPDATE principals SET authorization_version = authorization_version + 1 WHERE id = NEW.principal_id
