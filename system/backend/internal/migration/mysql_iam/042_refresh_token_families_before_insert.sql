CREATE TRIGGER IF NOT EXISTS trg_refresh_token_families_before_insert
BEFORE INSERT ON refresh_token_families FOR EACH ROW
BEGIN
    DECLARE subject_type VARCHAR(32) DEFAULT NULL;
    DECLARE subject_status VARCHAR(16);
    DECLARE subject_version BIGINT;
    SELECT principal_type, status, authorization_version INTO subject_type, subject_status, subject_version
      FROM principals WHERE id = NEW.principal_id FOR UPDATE;
    IF NEW.revoked_at IS NOT NULL OR subject_type IS NULL OR subject_status <> 'active'
       OR NEW.issued_authorization_version <> subject_version THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'token family requires an active principal at its current authorization version';
    END IF;
    IF NEW.context_type = 'platform' THEN
        IF (subject_type = 'user' AND NEW.assurance_level NOT IN ('aal2', 'aal3'))
           OR NOT EXISTS (
               SELECT 1 FROM role_assignments
                WHERE principal_id = NEW.principal_id AND scope_type = 'platform' AND status = 'active'
                  AND valid_from <= CURRENT_TIMESTAMP(6) AND (valid_until IS NULL OR valid_until > CURRENT_TIMESTAMP(6))
                FOR SHARE
           ) THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'platform token family requires an effective role and assurance';
        END IF;
    ELSEIF NOT EXISTS (
        SELECT 1 FROM tenant_memberships AS membership
        JOIN tenants AS tenant ON tenant.id = membership.tenant_id
        WHERE membership.id = NEW.tenant_membership_id AND membership.principal_id = NEW.principal_id
          AND membership.status = 'active' AND tenant.status = 'active'
          AND (membership.expires_at IS NULL OR membership.expires_at > CURRENT_TIMESTAMP(6))
        FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'tenant token family requires an active membership for the principal';
    END IF;
END
