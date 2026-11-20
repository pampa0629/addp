CREATE TRIGGER IF NOT EXISTS trg_tenant_memberships_validate_service_owner
BEFORE INSERT ON tenant_memberships FOR EACH ROW
BEGIN
    DECLARE service_id BIGINT DEFAULT NULL;
    SELECT id INTO service_id FROM principals WHERE id = NEW.principal_id AND principal_type = 'service_principal' FOR UPDATE;
    IF service_id IS NOT NULL AND EXISTS (
        SELECT 1 FROM service_principals
         WHERE id = NEW.principal_id AND owner_scope = 'tenant' AND owner_tenant_id <> NEW.tenant_id
         FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'tenant service principal cannot join another tenant';
    END IF;
END
