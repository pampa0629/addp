CREATE TRIGGER IF NOT EXISTS trg_service_principals_before_insert
BEFORE INSERT ON service_principals FOR EACH ROW
BEGIN
    DECLARE locked_principal BIGINT;
    IF NEW.owner_scope = 'tenant' THEN
        SELECT id INTO locked_principal FROM principals WHERE id = NEW.id FOR UPDATE;
        IF NOT EXISTS (
            SELECT 1 FROM tenant_memberships
             WHERE tenant_id = NEW.owner_tenant_id AND principal_id = NEW.id AND status = 'active'
               AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP(6))
             FOR SHARE
        ) OR EXISTS (
            SELECT 1 FROM tenant_memberships
             WHERE tenant_id <> NEW.owner_tenant_id AND principal_id = NEW.id
             FOR SHARE
        ) THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'tenant service principal requires membership in its owner tenant only';
        END IF;
    END IF;
END
