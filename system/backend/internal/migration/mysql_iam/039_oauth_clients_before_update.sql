CREATE TRIGGER IF NOT EXISTS trg_oauth_clients_before_update
BEFORE UPDATE ON oauth_clients FOR EACH ROW
BEGIN
    IF NEW.client_id <> OLD.client_id OR NEW.owner_scope <> OLD.owner_scope
       OR NOT (NEW.owner_tenant_id <=> OLD.owner_tenant_id)
       OR NOT (NEW.service_principal_id <=> OLD.service_principal_id)
       OR NOT (NEW.created_by_principal_id <=> OLD.created_by_principal_id)
       OR NEW.created_at <> OLD.created_at THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'OAuth client identity, binding and ownership are immutable';
    END IF;
    CALL validate_oauth_service_client(NEW.service_principal_id, NEW.owner_scope, NEW.owner_tenant_id, NEW.status);
    SET NEW.updated_at = CURRENT_TIMESTAMP(6);
END
