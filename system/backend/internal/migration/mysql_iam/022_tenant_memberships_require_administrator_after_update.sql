CREATE TRIGGER IF NOT EXISTS trg_tenant_memberships_require_administrator_after_update
AFTER UPDATE ON tenant_memberships FOR EACH ROW
BEGIN
    IF OLD.status = 'active' AND OLD.expires_at IS NULL
       AND (NEW.status <> 'active' OR NEW.expires_at IS NOT NULL) THEN
        CALL require_tenant_administrator(NEW.tenant_id, NULL, NULL);
    END IF;
END
