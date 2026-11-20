CREATE TRIGGER IF NOT EXISTS trg_departments_status_after_update
AFTER UPDATE ON departments FOR EACH ROW
BEGIN
    IF NEW.status='disabled' AND OLD.status<>NEW.status THEN
        UPDATE principals p SET p.authorization_version=p.authorization_version+1
        WHERE EXISTS (SELECT 1 FROM tenant_memberships tm JOIN department_memberships m ON m.tenant_membership_id=tm.id
                      WHERE tm.principal_id=p.id AND m.department_id=NEW.id AND m.status='active');
    END IF;
END
