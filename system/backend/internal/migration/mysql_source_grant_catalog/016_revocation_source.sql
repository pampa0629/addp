CREATE TRIGGER IF NOT EXISTS source_revocation_binding
BEFORE INSERT ON engine_access_grant_revocations FOR EACH ROW
BEGIN
    SET NEW.revoked_at=SYSDATE(6);
    IF NOT EXISTS (SELECT 1 FROM engine_access_grants g
        JOIN tenant_memberships m ON m.tenant_id=g.tenant_id JOIN principals p ON p.id=m.principal_id
        WHERE g.request_id=NEW.request_id AND g.granted_at<=NEW.revoked_at AND m.id=NEW.revoked_by_membership_id
          AND p.id=NEW.revoked_by_principal_id AND p.principal_type='user') THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='engine_access_grant_revoker_binding';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM engine_access_grants g WHERE g.request_id=NEW.request_id
        AND ((g.expiry_mode='until_revoked' AND g.expires_at IS NULL) OR (g.expiry_mode='at_time' AND g.expires_at>NEW.revoked_at))) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='engine_access_grant_revocation_expiry';
    END IF;
END
