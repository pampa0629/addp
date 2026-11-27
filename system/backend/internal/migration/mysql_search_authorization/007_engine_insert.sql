CREATE TRIGGER IF NOT EXISTS search_engine_insert_revision
AFTER INSERT ON engines FOR EACH ROW
BEGIN
    IF NEW.tenant_id IS NOT NULL THEN
        INSERT INTO search_authorization_revisions(tenant_id,version) VALUES (NEW.tenant_id,1)
        ON DUPLICATE KEY UPDATE version=search_authorization_revisions.version+1;
    END IF;
END
