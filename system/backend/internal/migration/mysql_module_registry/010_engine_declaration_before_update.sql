CREATE TRIGGER IF NOT EXISTS engine_deployment_declaration_close
BEFORE UPDATE ON engine_deployment_declarations FOR EACH ROW
BEGIN
    IF OLD.valid_to IS NOT NULL OR NEW.valid_to IS NULL OR NEW.deployment_id<>OLD.deployment_id
       OR NEW.version<>OLD.version OR NEW.node_id<>OLD.node_id OR NEW.inventory<>OLD.inventory
       OR NEW.enabled<>OLD.enabled OR NEW.valid_from<>OLD.valid_from THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='deployment declaration history is immutable';
    END IF;
END
