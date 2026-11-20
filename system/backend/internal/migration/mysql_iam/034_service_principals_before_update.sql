CREATE TRIGGER IF NOT EXISTS trg_service_principals_before_update
BEFORE UPDATE ON service_principals FOR EACH ROW
BEGIN
    IF NEW.id <> OLD.id OR NEW.owner_scope <> OLD.owner_scope
       OR NOT (NEW.owner_tenant_id <=> OLD.owner_tenant_id) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'service principal identity and ownership are immutable';
    END IF;
    SET NEW.updated_at = CURRENT_TIMESTAMP(6);
END
