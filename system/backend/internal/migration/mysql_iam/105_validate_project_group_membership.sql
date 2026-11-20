CREATE PROCEDURE IF NOT EXISTS validate_project_group_membership(IN target_tenant BIGINT, IN target_object BIGINT, IN target_member BIGINT, IN member_status VARCHAR(16))
READS SQL DATA
BEGIN
    IF member_status='active' AND NOT EXISTS (
        SELECT 1 FROM tenant_memberships tm JOIN principals p ON p.id=tm.principal_id
        JOIN project_groups o ON o.id=target_object AND o.tenant_id=tm.tenant_id
        WHERE tm.id=target_member AND tm.tenant_id=target_tenant
          AND tm.status='active' AND p.status='active' AND o.status='active' AND (tm.expires_at IS NULL OR tm.expires_at>CURRENT_TIMESTAMP(6))
        FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='ineligible project_group membership';
    END IF;
END
