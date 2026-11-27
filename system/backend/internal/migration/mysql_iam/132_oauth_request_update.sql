CREATE TRIGGER IF NOT EXISTS oauth_request_validate_update
BEFORE UPDATE ON oauth_authorization_requests FOR EACH ROW
BEGIN
    IF OLD.status<>'pending' OR NEW.id<>OLD.id OR NEW.request_secret_hash<>OLD.request_secret_hash
        OR NEW.client_id<>OLD.client_id OR NEW.redirect_uri<>OLD.redirect_uri OR NEW.response_types<>OLD.response_types
        OR NEW.response_mode<>OLD.response_mode OR NEW.requested_scopes<>OLD.requested_scopes
        OR NEW.requested_audiences<>OLD.requested_audiences OR NEW.requested_at<>OLD.requested_at
        OR NEW.expires_at<>OLD.expires_at OR NEW.created_at<>OLD.created_at THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='OAuth request protocol facts and terminal state are immutable';
    END IF;
    IF NEW.completed_at>CURRENT_TIMESTAMP(6) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='OAuth completion cannot be in the future';
    END IF;
    IF NEW.status='approved' AND (NEW.authenticated_at>NEW.completed_at
        OR NOT JSON_CONTAINS(NEW.requested_scopes,NEW.granted_scopes)
        OR NOT JSON_CONTAINS(NEW.requested_audiences,NEW.granted_audiences)
        OR NOT EXISTS (SELECT 1 FROM oauth_clients WHERE client_id=NEW.client_id AND status='active'
            AND JSON_CONTAINS(grant_types,JSON_QUOTE('authorization_code'))
            AND JSON_CONTAINS(allowed_scopes,NEW.requested_scopes)
            AND JSON_CONTAINS(allowed_audiences,NEW.requested_audiences) FOR SHARE)
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
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='Invalid client, identity or grants for OAuth approval';
    END IF;
END
