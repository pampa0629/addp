CREATE TRIGGER IF NOT EXISTS trg_tenants_require_administrator_after_update
AFTER UPDATE ON tenants FOR EACH ROW
BEGIN
    IF NEW.initialized_at IS NOT NULL AND NEW.status <> 'closed'
       AND (OLD.initialized_at IS NULL OR OLD.status = 'closed') THEN
        CALL require_tenant_administrator(NEW.id, NULL, NULL);
    END IF;
END
