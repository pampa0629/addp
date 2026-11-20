CREATE TRIGGER IF NOT EXISTS trg_department_memberships_before_insert
BEFORE INSERT ON department_memberships FOR EACH ROW
CALL validate_department_membership(NEW.tenant_id,NEW.department_id,NEW.tenant_membership_id,NEW.status)
