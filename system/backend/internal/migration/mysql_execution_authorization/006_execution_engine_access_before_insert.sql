CREATE TRIGGER IF NOT EXISTS execution_engine_access_validate_insert
BEFORE INSERT ON execution_authorization_engine_accesses FOR EACH ROW
BEGIN
    DECLARE parent_tenant BIGINT DEFAULT NULL;
    DECLARE parent_sealed DATETIME(6);
    DECLARE parent_source VARCHAR(24);
    SELECT tenant_id, sealed_at, source_type INTO parent_tenant, parent_sealed, parent_source
    FROM execution_authorizations WHERE id=NEW.authorization_id FOR UPDATE;
    IF parent_tenant IS NULL OR parent_sealed IS NOT NULL THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='execution authorization access boundary is already sealed or missing';
    END IF;
    IF (parent_source='service_definition' AND NEW.effects<>JSON_ARRAY('read'))
       OR JSON_LENGTH(NEW.effects)<>(
           SELECT COUNT(DISTINCT items.effect) FROM JSON_TABLE(NEW.effects, '$[*]'
             COLUMNS (effect VARCHAR(20) PATH '$' ERROR ON ERROR)) items
       ) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='invalid execution authorization engine effects';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM engines e WHERE e.id=NEW.engine_id AND e.lifecycle_state='active'
       AND (e.tenant_id=parent_tenant OR (e.tenant_id IS NULL AND e.is_builtin=TRUE)) FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='execution authorization contains an unavailable or cross-tenant engine';
    END IF;
END
