CREATE TRIGGER IF NOT EXISTS trg_principals_require_tenant_administrator
AFTER UPDATE ON principals FOR EACH ROW
BEGIN
    IF OLD.status = 'active' AND NEW.status <> 'active' THEN
        CALL require_tenant_administrator(NULL, NEW.id, NULL);
    END IF;
END
