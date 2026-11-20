CREATE PROCEDURE IF NOT EXISTS require_tenant_administrator(
    IN target_tenant BIGINT, IN affected_principal BIGINT, IN affected_role BIGINT
)
READS SQL DATA
BEGIN
    DECLARE invalid_tenant BIGINT DEFAULT NULL;
    DECLARE CONTINUE HANDLER FOR NOT FOUND SET invalid_tenant = NULL;

    SELECT t.id INTO invalid_tenant
      FROM tenants AS t
     WHERE t.initialized_at IS NOT NULL AND t.status <> 'closed'
       AND (target_tenant IS NULL OR t.id = target_tenant)
       AND (
           (affected_principal IS NULL AND affected_role IS NULL)
           OR EXISTS (
               SELECT 1
                 FROM role_assignments AS affected
                WHERE affected.tenant_id = t.id AND affected.scope_type = 'tenant'
                  AND affected.status = 'active' AND affected.valid_until IS NULL
                  AND affected.valid_from <= CURRENT_TIMESTAMP(6)
                  AND (affected_principal IS NULL OR affected.principal_id = affected_principal)
                  AND (affected_role IS NULL OR affected.role_id = affected_role)
                FOR SHARE
           )
       )
       AND NOT EXISTS (
           SELECT 1
             FROM role_assignments AS assignment
             JOIN roles AS role ON role.id = assignment.role_id
             JOIN principals AS principal ON principal.id = assignment.principal_id
             JOIN tenant_memberships AS membership
               ON membership.principal_id = assignment.principal_id AND membership.tenant_id = assignment.tenant_id
            WHERE assignment.tenant_id = t.id
              AND assignment.scope_type = 'tenant' AND assignment.status = 'active'
              AND assignment.valid_from <= CURRENT_TIMESTAMP(6) AND assignment.valid_until IS NULL
              AND role.tenant_id IS NULL AND role.role_key = 'tenant.administrator' AND role.status = 'active'
              AND principal.status = 'active' AND membership.status = 'active' AND membership.expires_at IS NULL
            LIMIT 1 FOR SHARE
       )
     LIMIT 1 FOR SHARE;

    IF invalid_tenant IS NOT NULL THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO = 30001,
            MESSAGE_TEXT = 'tenant must retain an effective tenant administrator';
    END IF;
END
