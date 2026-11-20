CREATE TRIGGER IF NOT EXISTS trg_permissions_after_update
AFTER UPDATE ON permissions FOR EACH ROW
UPDATE principals p SET p.authorization_version=p.authorization_version+1
WHERE EXISTS (SELECT 1 FROM role_assignments a JOIN role_permissions b ON b.role_id=a.role_id
              WHERE b.permission_id=NEW.id AND a.principal_id=p.id AND a.status='active')
