CREATE TRIGGER IF NOT EXISTS trg_role_assignments_before_insert
BEFORE INSERT ON role_assignments FOR EACH ROW
BEGIN
    DECLARE assignee_type VARCHAR(32) DEFAULT NULL;
    DECLARE assignee_status VARCHAR(16) DEFAULT NULL;

    IF NEW.status <> 'active' OR (NEW.source_type <> 'bootstrap' AND NEW.grant_reason IS NULL) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'new role assignment must be active and have its required grant reason';
    END IF;

    SELECT principal_type, status INTO assignee_type, assignee_status
      FROM principals WHERE id = NEW.principal_id FOR UPDATE;
    IF assignee_type IS NULL OR assignee_status <> 'active'
       OR NOT EXISTS (
           SELECT 1 FROM roles
            WHERE id = NEW.role_id AND status = 'active'
              AND JSON_CONTAINS(allowed_principal_types, JSON_QUOTE(assignee_type))
              AND JSON_CONTAINS(allowed_scope_types, JSON_QUOTE(NEW.scope_type))
              AND (tenant_id IS NULL OR tenant_id = NEW.tenant_id)
            FOR SHARE
       ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'role assignment requires an eligible active principal and role';
    END IF;

    IF NEW.scope_type = 'tenant' AND NOT EXISTS (
        SELECT 1 FROM tenant_memberships
         WHERE tenant_id = NEW.tenant_id AND principal_id = NEW.principal_id AND status = 'active'
           AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP(6))
         FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'tenant scope role assignment requires an active membership';
    END IF;

    IF NEW.scope_type = 'platform' AND NEW.source_type <> 'bootstrap' THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'platform assignment requires the privileged request migration';
    END IF;

    IF EXISTS (
        SELECT 1 FROM role_assignments
         WHERE principal_id = NEW.principal_id AND role_id = NEW.role_id
           AND scope_type = NEW.scope_type AND (tenant_id <=> NEW.tenant_id) AND status = 'active'
           AND (NEW.valid_until IS NULL OR valid_from < NEW.valid_until)
           AND (valid_until IS NULL OR NEW.valid_from < valid_until)
         FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '23000' SET MYSQL_ERRNO = 1062,
            MESSAGE_TEXT = 'role assignment validity interval overlaps an existing assignment';
    END IF;
END
