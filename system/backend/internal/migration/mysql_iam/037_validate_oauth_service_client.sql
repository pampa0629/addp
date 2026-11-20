CREATE PROCEDURE IF NOT EXISTS validate_oauth_service_client(
    IN service_id BIGINT, IN client_owner_scope VARCHAR(16), IN client_owner_tenant BIGINT, IN client_status VARCHAR(16)
)
READS SQL DATA
BEGIN
    IF service_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM service_principals AS service
        JOIN principals AS principal ON principal.id = service.id
        WHERE service.id = service_id AND service.owner_scope = client_owner_scope
          AND (service.owner_tenant_id <=> client_owner_tenant)
          AND (
              client_status <> 'active' OR (principal.status = 'active' AND (
                  service.owner_scope = 'platform' OR EXISTS (
                      SELECT 1 FROM tenant_memberships
                      WHERE tenant_id = service.owner_tenant_id AND principal_id = service.id AND status = 'active'
                        AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP(6))
                      FOR SHARE
                  )
              ))
          )
        FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'OAuth service client must match service principal ownership and active identity';
    END IF;
END
