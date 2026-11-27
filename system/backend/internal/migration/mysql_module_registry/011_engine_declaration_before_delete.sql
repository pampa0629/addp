CREATE TRIGGER IF NOT EXISTS engine_deployment_declaration_no_delete
BEFORE DELETE ON engine_deployment_declarations FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='deployment declaration history is immutable'
