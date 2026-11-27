CREATE TRIGGER IF NOT EXISTS notebook_session_validate_insert
BEFORE INSERT ON notebook_session_authorizations FOR EACH ROW
BEGIN
    IF NOT EXISTS (SELECT 1 FROM refresh_token_families f
        JOIN principals p ON p.id=f.principal_id
        JOIN tenant_memberships m ON m.id=f.tenant_membership_id AND m.principal_id=p.id
        JOIN tenants t ON t.id=m.tenant_id
        WHERE f.id=NEW.token_family_id AND p.id=NEW.actor_principal_id AND p.principal_type='user'
          AND p.status='active' AND p.authorization_version=NEW.issued_authorization_version
          AND f.context_type='tenant' AND f.revoked_at IS NULL AND f.expires_at>=NEW.expires_at
          AND m.id=NEW.tenant_membership_id AND m.status='active' AND (m.expires_at IS NULL OR m.expires_at>NEW.created_at)
          AND t.id=NEW.tenant_id AND t.status='active' FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='Notebook session requires its active tenant user token family';
    END IF;
END
