CREATE TRIGGER IF NOT EXISTS oauth_device_validate_update
BEFORE UPDATE ON oauth_device_authorizations FOR EACH ROW
BEGIN
    IF OLD.status IN ('rejected','invalidated')
        OR (OLD.status='approved' AND NEW.status NOT IN ('approved','invalidated'))
        OR (OLD.status='pending' AND NEW.status='invalidated')
        OR NEW.id<>OLD.id OR NEW.device_code_hash<>OLD.device_code_hash OR NEW.user_code_hash<>OLD.user_code_hash
        OR NEW.client_id<>OLD.client_id OR NEW.requested_scopes<>OLD.requested_scopes
        OR NEW.requested_audiences<>OLD.requested_audiences OR NEW.requested_at<>OLD.requested_at
        OR NEW.expires_at<>OLD.expires_at OR NEW.created_at<>OLD.created_at
        OR NEW.poll_interval_seconds<OLD.poll_interval_seconds OR NEW.next_poll_at<OLD.next_poll_at
        OR NEW.last_polled_at<OLD.last_polled_at
        OR (OLD.decided_at IS NOT NULL AND NOT (NEW.decided_at <=> OLD.decided_at))
        OR (OLD.invalidated_at IS NOT NULL AND NOT (NEW.invalidated_at <=> OLD.invalidated_at)) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='Device protocol and terminal facts are immutable';
    END IF;
    IF NEW.status='approved' AND NOT EXISTS (SELECT 1 FROM oauth_clients WHERE client_id=NEW.client_id AND status='active'
            AND JSON_CONTAINS(grant_types,JSON_QUOTE('urn:ietf:params:oauth:grant-type:device_code'))
            AND JSON_CONTAINS(allowed_scopes,NEW.requested_scopes)
            AND JSON_CONTAINS(allowed_audiences,NEW.requested_audiences) FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='Device approval exceeds the active client registration';
    END IF;
    IF NEW.decided_at>CURRENT_TIMESTAMP(6) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='Device authorization decision cannot be in the future';
    END IF;
    IF NEW.status IN ('approved','invalidated') AND (NEW.authenticated_at>NEW.decided_at
        OR NOT JSON_CONTAINS(NEW.requested_scopes,NEW.granted_scopes)
        OR NOT JSON_CONTAINS(NEW.requested_audiences,NEW.granted_audiences)
        OR NOT EXISTS (SELECT 1 FROM principals p WHERE p.id=NEW.principal_id AND p.principal_type='user'
            AND p.status='active' AND p.authorization_version=NEW.issued_authorization_version
            AND ((NEW.context_type='platform' AND NEW.assurance_level IN ('aal2','aal3')
                AND EXISTS (SELECT 1 FROM role_assignments r WHERE r.principal_id=p.id AND r.scope_type='platform'
                    AND r.status='active' AND r.valid_from<=CURRENT_TIMESTAMP(6)
                    AND (r.valid_until IS NULL OR r.valid_until>CURRENT_TIMESTAMP(6)) FOR SHARE))
                OR (NEW.context_type='tenant' AND EXISTS (
                    SELECT 1 FROM tenant_memberships m JOIN tenants t ON t.id=m.tenant_id
                    WHERE m.id=NEW.tenant_membership_id AND m.principal_id=p.id AND m.status='active'
                    AND t.status='active' AND (m.expires_at IS NULL OR m.expires_at>CURRENT_TIMESTAMP(6)) FOR SHARE)))
            FOR SHARE)) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='Invalid device authorization identity or grants';
    END IF;
END
