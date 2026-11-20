CREATE TRIGGER IF NOT EXISTS trg_department_memberships_after_insert
AFTER INSERT ON department_memberships FOR EACH ROW
CALL bump_organization_member(NEW.tenant_membership_id)
