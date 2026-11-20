CREATE TRIGGER IF NOT EXISTS trg_role_assignment_conflicts_before_insert
BEFORE INSERT ON role_assignments FOR EACH ROW
BEGIN
    IF EXISTS (
        SELECT 1 FROM role_assignments a JOIN role_conflicts c
          ON c.role_id_low = LEAST(a.role_id, NEW.role_id) AND c.role_id_high = GREATEST(a.role_id, NEW.role_id)
        WHERE a.principal_id = NEW.principal_id AND a.status = 'active'
          AND (a.valid_until IS NULL OR a.valid_until > CURRENT_TIMESTAMP(6)) FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'principal already has a conflicting active role';
    END IF;
END
