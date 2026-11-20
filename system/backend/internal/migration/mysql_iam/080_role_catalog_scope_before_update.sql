CREATE TRIGGER IF NOT EXISTS trg_role_catalog_scope_before_update
BEFORE UPDATE ON roles FOR EACH ROW
BEGIN
    IF EXISTS (SELECT 1 FROM role_permissions b JOIN permissions p ON p.id=b.permission_id
               WHERE b.role_id=OLD.id AND NOT JSON_CONTAINS(p.allowed_scope_types,NEW.allowed_scope_types) FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='role scope exceeds an existing permission';
    END IF;
    IF EXISTS (SELECT 1 FROM role_assignments a JOIN principals p ON p.id=a.principal_id
               WHERE a.role_id=OLD.id AND a.status='active'
                 AND (NOT JSON_CONTAINS(NEW.allowed_principal_types,JSON_QUOTE(p.principal_type))
                      OR NOT JSON_CONTAINS(NEW.allowed_scope_types,JSON_QUOTE(a.scope_type))) FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='role scope or principal type excludes an active assignment';
    END IF;
END
