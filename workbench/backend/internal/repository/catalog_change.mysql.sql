CREATE TRIGGER IF NOT EXISTS trg_workbench_data_application_catalog_change
AFTER UPDATE ON data_applications FOR EACH ROW
BEGIN
    IF NEW.current_revision_number IS NOT NULL AND
       (NOT (NEW.current_revision_number <=> OLD.current_revision_number) OR NEW.publication_status <> OLD.publication_status) THEN
        INSERT INTO catalog_resource_changes (tenant_id, source_type, source_identity, operation, snapshot, observed_at)
        SELECT NEW.tenant_id, 'data_application', NEW.id, 'upsert',
               JSON_OBJECT('name', revision.name, 'description', revision.description,
                           'object_kind', 'data_application', 'publication_status', NEW.publication_status,
                           'revision_number', NEW.current_revision_number, 'runtime_path', CONCAT('/data-apps/', NEW.id)), NOW(6)
        FROM data_application_revisions AS revision
        WHERE revision.tenant_id = NEW.tenant_id AND revision.application_id = NEW.id
          AND revision.revision_number = NEW.current_revision_number;
        IF ROW_COUNT() <> 1 THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'current Workbench application revision is missing';
        END IF;
    END IF;
END
