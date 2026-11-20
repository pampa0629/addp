CREATE TRIGGER IF NOT EXISTS trg_role_assignments_after_update
AFTER UPDATE ON role_assignments FOR EACH ROW
BEGIN
    IF OLD.scope_type = 'tenant' AND OLD.valid_until IS NULL AND OLD.valid_from <= CURRENT_TIMESTAMP(6) THEN
        CALL require_tenant_administrator(OLD.tenant_id, NULL, NULL);
    END IF;
    UPDATE principals SET authorization_version = authorization_version + 1 WHERE id = NEW.principal_id;
END
