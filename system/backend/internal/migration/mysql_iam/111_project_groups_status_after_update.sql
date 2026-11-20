CREATE TRIGGER IF NOT EXISTS trg_project_groups_status_after_update
AFTER UPDATE ON project_groups FOR EACH ROW
BEGIN
    IF NEW.status='closed' AND OLD.status<>NEW.status THEN
        UPDATE principals p SET p.authorization_version=p.authorization_version+1
        WHERE EXISTS (SELECT 1 FROM tenant_memberships tm JOIN project_group_memberships m ON m.tenant_membership_id=tm.id
                      WHERE tm.principal_id=p.id AND m.project_group_id=NEW.id AND m.status='active');
    END IF;
END
