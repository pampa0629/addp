CREATE TRIGGER IF NOT EXISTS trg_tenant_memberships_require_administrator_after_delete
AFTER DELETE ON tenant_memberships FOR EACH ROW
BEGIN
    IF OLD.status = 'active' AND OLD.expires_at IS NULL THEN
        CALL require_tenant_administrator(OLD.tenant_id, NULL, NULL);
    END IF;
END
