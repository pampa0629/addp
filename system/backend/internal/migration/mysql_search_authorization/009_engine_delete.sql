CREATE TRIGGER IF NOT EXISTS search_engine_delete_revision
AFTER DELETE ON engines FOR EACH ROW
BEGIN
    IF OLD.tenant_id IS NOT NULL THEN
        INSERT INTO search_authorization_revisions(tenant_id,version) VALUES (OLD.tenant_id,1)
        ON DUPLICATE KEY UPDATE version=search_authorization_revisions.version+1;
    END IF;
END
