CREATE TRIGGER IF NOT EXISTS execution_internal_task_validate_insert
BEFORE INSERT ON execution_authorizations FOR EACH ROW
BEGIN
    IF NEW.internal_task IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM common.task_executions e
        WHERE e.execution_id=NEW.execution_id AND e.tenant_id=NEW.tenant_id
          AND e.module='ontology' AND e.source='ontology' AND e.task_type='semantic_projection'
          AND e.execution_boundary='bounded' AND e.status='pending' AND e.execution_authorization_id IS NULL
          AND e.actor_principal_id=NEW.actor_principal_id AND e.actor_tenant_membership_id=NEW.tenant_membership_id
          AND e.issued_authorization_version=NEW.issued_authorization_version
          AND e.execution_config=JSON_OBJECT(
              'ontology_id', JSON_EXTRACT(NEW.internal_task,'$.resource_id'),
              'revision', JSON_EXTRACT(NEW.internal_task,'$.revision'),
              'digest', JSON_EXTRACT(NEW.internal_task,'$.digest'),
              'generation', JSON_EXTRACT(NEW.internal_task,'$.generation'))
        FOR UPDATE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='internal authorization requires the exact pending owner execution';
    END IF;
END
