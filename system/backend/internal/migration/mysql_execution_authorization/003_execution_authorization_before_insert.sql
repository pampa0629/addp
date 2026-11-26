CREATE TRIGGER IF NOT EXISTS execution_authorization_validate_insert
BEFORE INSERT ON execution_authorizations FOR EACH ROW
BEGIN
    IF NEW.sealed_at IS NOT NULL THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='new execution authorization must be unsealed';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM principals p
        JOIN tenant_memberships m ON m.id=NEW.tenant_membership_id AND m.principal_id=p.id AND m.tenant_id=NEW.tenant_id
        JOIN tenants t ON t.id=m.tenant_id
        WHERE p.id=NEW.actor_principal_id AND p.status='active' AND p.authorization_version=NEW.issued_authorization_version
          AND m.status='active' AND (m.expires_at IS NULL OR m.expires_at>NEW.created_at) AND t.status='active'
          AND ((NEW.source_type='user' AND p.principal_type='user')
            OR (NEW.source_type='service_definition' AND p.principal_type='service_principal'
                AND NEW.audience='duckdb' AND NEW.expires_at<=NEW.created_at+INTERVAL 1 MINUTE
                AND EXISTS (SELECT 1 FROM service_principals sp WHERE sp.id=p.id AND sp.name='addp-service' AND sp.owner_scope='platform' FOR SHARE)))
        FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='execution authorization requires an active matching tenant subject';
    END IF;
END
