CREATE TRIGGER IF NOT EXISTS trg_tenant_memberships_after_update
AFTER UPDATE ON tenant_memberships FOR EACH ROW
UPDATE principals SET authorization_version = authorization_version + 1 WHERE id = NEW.principal_id
