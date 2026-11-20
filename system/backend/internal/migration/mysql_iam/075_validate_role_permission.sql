CREATE PROCEDURE IF NOT EXISTS validate_role_permission(
    IN target_role BIGINT, IN target_permission BIGINT, IN binding_source VARCHAR(16), IN creator BIGINT
)
READS SQL DATA
BEGIN
    DECLARE role_tenant BIGINT;
    DECLARE eligible BOOLEAN DEFAULT FALSE;
    SELECT r.tenant_id, r.status='active' AND p.status='active'
           AND JSON_CONTAINS(p.allowed_scope_types, r.allowed_scope_types)
           AND ((binding_source='product' AND r.role_type IN ('tenant_builtin','platform_builtin'))
             OR (binding_source='tenant' AND r.role_type='tenant_custom' AND p.tenant_customizable))
      INTO role_tenant, eligible
      FROM roles r JOIN permissions p ON p.id=target_permission
     WHERE r.id=target_role FOR SHARE;
    IF NOT eligible THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='role or permission is ineligible for this binding';
    END IF;
    IF binding_source='tenant' AND NOT EXISTS (
        SELECT 1 FROM tenant_memberships m JOIN principals p ON p.id=m.principal_id
        WHERE m.tenant_id=role_tenant AND m.principal_id=creator AND m.status='active' AND p.status='active'
          AND (m.expires_at IS NULL OR m.expires_at>CURRENT_TIMESTAMP(6)) FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='role permission creator requires an active membership in the role tenant';
    END IF;
END
