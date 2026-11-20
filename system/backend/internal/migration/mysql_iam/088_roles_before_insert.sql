CREATE TRIGGER IF NOT EXISTS trg_roles_before_insert
BEFORE INSERT ON roles FOR EACH ROW
BEGIN
    IF NEW.role_type = 'tenant_custom' AND NOT EXISTS (
        SELECT 1 FROM tenant_memberships m JOIN principals p ON p.id=m.principal_id
        WHERE m.tenant_id=NEW.tenant_id AND m.principal_id=NEW.created_by_principal_id
          AND m.status='active' AND p.status='active'
          AND (m.expires_at IS NULL OR m.expires_at>CURRENT_TIMESTAMP(6)) FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='custom role creator requires an active membership in the role tenant';
    END IF;
END
