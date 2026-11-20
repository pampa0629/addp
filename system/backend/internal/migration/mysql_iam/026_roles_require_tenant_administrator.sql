CREATE TRIGGER IF NOT EXISTS trg_roles_require_tenant_administrator
AFTER UPDATE ON roles FOR EACH ROW
BEGIN
    IF OLD.status = 'active' AND NEW.status <> 'active'
       AND OLD.tenant_id IS NULL AND OLD.role_key = 'tenant.administrator' THEN
        CALL require_tenant_administrator(NULL, NULL, NEW.id);
    END IF;
END
