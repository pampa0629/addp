CREATE TRIGGER IF NOT EXISTS process_metrics_declaration_immutable
BEFORE UPDATE ON module_runtime_instances FOR EACH ROW
BEGIN
    IF NOT (NEW.process_metrics <=> OLD.process_metrics) OR (OLD.process_metrics IS NOT NULL AND
        (NEW.role <> OLD.role OR NOT (NEW.process_started_at <=> OLD.process_started_at))) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='process metrics declaration is immutable';
    END IF;
END
