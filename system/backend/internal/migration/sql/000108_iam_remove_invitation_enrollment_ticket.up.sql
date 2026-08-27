CREATE FUNCTION system.prevent_tenant_invitation_delete()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'tenant invitation history cannot be physically deleted';
END;
$$;

DROP TRIGGER trg_tenant_invitations_prevent_delete ON system.tenant_invitations;
DROP TRIGGER trg_tenant_invitations_prevent_truncate ON system.tenant_invitations;

CREATE TRIGGER trg_tenant_invitations_prevent_delete
BEFORE DELETE ON system.tenant_invitations
FOR EACH ROW EXECUTE FUNCTION system.prevent_tenant_invitation_delete();

CREATE TRIGGER trg_tenant_invitations_prevent_truncate
BEFORE TRUNCATE ON system.tenant_invitations
FOR EACH STATEMENT EXECUTE FUNCTION system.prevent_tenant_invitation_delete();

DROP TABLE system.enrollment_tickets;
DROP FUNCTION system.validate_enrollment_ticket_transition();
DROP FUNCTION system.prevent_invitation_enrollment_delete();

ALTER TABLE system.iam_security_policy
    DROP COLUMN enrollment_ticket_ttl_minutes;
