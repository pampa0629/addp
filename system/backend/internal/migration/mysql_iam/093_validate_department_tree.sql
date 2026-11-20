CREATE PROCEDURE IF NOT EXISTS validate_department_tree(IN tenant BIGINT, IN node BIGINT, IN parent_node BIGINT)
READS SQL DATA
BEGIN
    DECLARE locked_tenant BIGINT;
    SELECT id INTO locked_tenant FROM tenants WHERE id=tenant FOR UPDATE;
    IF parent_node IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM departments d WHERE d.tenant_id=tenant AND d.id=parent_node
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='department parent must exist in the same tenant';
    END IF;
    IF parent_node IS NOT NULL AND (parent_node=node OR EXISTS (
        WITH RECURSIVE ancestors(id,parent_id) AS (
            SELECT d.id,d.parent_id FROM departments d WHERE d.tenant_id=tenant AND d.id=parent_node
            UNION DISTINCT
            SELECT d.id,d.parent_id FROM departments d JOIN ancestors a ON d.id=a.parent_id WHERE d.tenant_id=tenant
        )
        SELECT 1 FROM ancestors WHERE id=node
    )) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='department hierarchy cannot contain a cycle';
    END IF;
END
