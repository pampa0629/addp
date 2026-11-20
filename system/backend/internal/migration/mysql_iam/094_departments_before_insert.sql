CREATE TRIGGER IF NOT EXISTS trg_departments_before_insert
BEFORE INSERT ON departments FOR EACH ROW
CALL validate_department_tree(NEW.tenant_id,NEW.id,NEW.parent_id)
