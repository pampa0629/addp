CREATE TRIGGER IF NOT EXISTS trg_permissions_before_update
BEFORE UPDATE ON permissions FOR EACH ROW
BEGIN
    IF NEW.id<>OLD.id OR NEW.permission_key<>OLD.permission_key OR NEW.owner_module<>OLD.owner_module
       OR NEW.action<>OLD.action OR NEW.created_at<>OLD.created_at THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='permission identity cannot change';
    END IF;
    IF EXISTS (SELECT 1 FROM role_permissions b JOIN roles r ON r.id=b.role_id
               WHERE b.permission_id=OLD.id AND NOT JSON_CONTAINS(NEW.allowed_scope_types,r.allowed_scope_types) FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='permission scope excludes an existing role';
    END IF;
    SET NEW.updated_at=CURRENT_TIMESTAMP(6);
END
