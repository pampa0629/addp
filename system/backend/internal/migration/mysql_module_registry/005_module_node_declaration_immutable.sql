CREATE TRIGGER IF NOT EXISTS module_node_declaration_immutable
BEFORE UPDATE ON module_runtime_instances FOR EACH ROW
BEGIN
    IF NEW.declared_node_id <> OLD.declared_node_id OR NEW.registration_client_id <> OLD.registration_client_id THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='module node declaration is immutable';
    END IF;
END
