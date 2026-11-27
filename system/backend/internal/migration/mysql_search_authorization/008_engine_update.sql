CREATE TRIGGER IF NOT EXISTS search_engine_update_revision
AFTER UPDATE ON engines FOR EACH ROW
BEGIN
    IF NOT (OLD.id <=> NEW.id AND OLD.tenant_id <=> NEW.tenant_id
        AND BINARY OLD.engine_type <=> BINARY NEW.engine_type
        AND BINARY OLD.lifecycle_state <=> BINARY NEW.lifecycle_state
        AND OLD.version <=> NEW.version AND OLD.deleted_at <=> NEW.deleted_at) THEN
        IF OLD.tenant_id IS NOT NULL THEN
            INSERT INTO search_authorization_revisions(tenant_id,version) VALUES (OLD.tenant_id,1)
        ON DUPLICATE KEY UPDATE version=search_authorization_revisions.version+1;
        END IF;
        IF NEW.tenant_id IS NOT NULL AND NOT (NEW.tenant_id <=> OLD.tenant_id) THEN
            INSERT INTO search_authorization_revisions(tenant_id,version) VALUES (NEW.tenant_id,1)
        ON DUPLICATE KEY UPDATE version=search_authorization_revisions.version+1;
        END IF;
    END IF;
END
