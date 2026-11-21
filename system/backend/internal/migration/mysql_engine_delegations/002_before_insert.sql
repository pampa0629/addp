CREATE TRIGGER IF NOT EXISTS engine_delegation_insert
BEFORE INSERT ON engine_access_delegations FOR EACH ROW
BEGIN
    DECLARE eligible_member BIGINT;
    -- Serialize checks on an existing membership even when its first delegation has no row.
    SELECT m.id INTO eligible_member FROM tenant_memberships m
        JOIN principals p ON p.id=m.principal_id JOIN tenants t ON t.id=m.tenant_id
        WHERE m.id=NEW.tenant_membership_id AND m.tenant_id=NEW.tenant_id
          AND m.status='active' AND p.principal_type='user' AND p.status='active' AND t.status='active'
          AND (m.expires_at IS NULL OR m.expires_at>=NEW.expires_at)
        FOR UPDATE OF m FOR SHARE OF p,t;
    IF eligible_member IS NULL THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='delegation requires an effective tenant user';
    END IF;
    IF NEW.expires_at<=SYSDATE(6) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='engine_access_delegations_expiry';
    END IF;
    IF NEW.status<>'active' OR NEW.version<>1 OR NEW.granted_at>SYSDATE(6) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='invalid initial engine access delegation';
    END IF;
    IF EXISTS (SELECT 1 FROM engine_access_delegations d
        WHERE d.tenant_id=NEW.tenant_id AND d.engine_id=NEW.engine_id
          AND d.tenant_membership_id=NEW.tenant_membership_id AND d.status='active'
          AND d.granted_at<NEW.expires_at AND NEW.granted_at<d.expires_at FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='engine_access_delegations_overlap';
    END IF;
END
