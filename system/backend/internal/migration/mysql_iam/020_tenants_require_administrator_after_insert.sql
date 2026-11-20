CREATE TRIGGER IF NOT EXISTS trg_tenants_require_administrator_after_insert
AFTER INSERT ON tenants FOR EACH ROW
BEGIN
    IF NEW.initialized_at IS NOT NULL AND NEW.status <> 'closed' THEN
        CALL require_tenant_administrator(NEW.id, NULL, NULL);
    END IF;
END
