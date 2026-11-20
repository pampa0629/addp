CREATE TRIGGER IF NOT EXISTS trg_oauth_clients_before_insert
BEFORE INSERT ON oauth_clients FOR EACH ROW
CALL validate_oauth_service_client(NEW.service_principal_id, NEW.owner_scope, NEW.owner_tenant_id, NEW.status)
