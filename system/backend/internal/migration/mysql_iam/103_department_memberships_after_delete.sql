CREATE TRIGGER IF NOT EXISTS trg_department_memberships_after_delete
AFTER DELETE ON department_memberships FOR EACH ROW
CALL bump_organization_member(OLD.tenant_membership_id)
