CREATE TRIGGER IF NOT EXISTS search_grant_insert_revision
AFTER INSERT ON engine_access_grants FOR EACH ROW
BEGIN
    INSERT INTO search_authorization_revisions(tenant_id,version) VALUES (NEW.tenant_id,1)
        ON DUPLICATE KEY UPDATE version=search_authorization_revisions.version+1;
END
