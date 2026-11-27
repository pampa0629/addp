CREATE TRIGGER IF NOT EXISTS search_revocation_insert_revision
AFTER INSERT ON engine_access_grant_revocations FOR EACH ROW
BEGIN
    INSERT INTO search_authorization_revisions(tenant_id,version)
    SELECT tenant_id,1 FROM engine_access_grants WHERE request_id=NEW.request_id
    ON DUPLICATE KEY UPDATE version=search_authorization_revisions.version+1;
END
