CREATE TRIGGER IF NOT EXISTS trg_tenant_memberships_before_update
BEFORE UPDATE ON tenant_memberships FOR EACH ROW
BEGIN
    IF NEW.tenant_id <> OLD.tenant_id OR NEW.principal_id <> OLD.principal_id THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'tenant membership identity cannot change';
    END IF;
    SET NEW.updated_at = CURRENT_TIMESTAMP(6);
END
