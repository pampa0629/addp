CREATE TRIGGER IF NOT EXISTS source_grant_active_relation
BEFORE INSERT ON engine_access_grants FOR EACH ROW
BEGIN
    INSERT INTO engine_access_target_locks(target_key) VALUES (UNHEX(SHA2(CAST(JSON_ARRAY('source_grant_relation',
        NEW.tenant_id,NEW.engine_id,NEW.catalog_path,NEW.node_recursive,NEW.recipient_type,NEW.recipient_id,NEW.action)
        AS CHAR CHARACTER SET utf8mb4),256))) ON DUPLICATE KEY UPDATE lock_marker=0;
    IF EXISTS (SELECT 1 FROM engine_access_grants g WHERE g.request_id<>NEW.request_id
        AND g.tenant_id=NEW.tenant_id AND g.engine_id=NEW.engine_id AND g.catalog_path=NEW.catalog_path
        AND g.node_recursive<=>NEW.node_recursive AND g.recipient_type=NEW.recipient_type AND g.recipient_id=NEW.recipient_id
        AND g.action=NEW.action AND g.granted_at<=SYSDATE(6) AND (g.expires_at IS NULL OR g.expires_at>SYSDATE(6))
        AND NOT EXISTS (SELECT 1 FROM engine_access_grant_revocations r WHERE r.request_id=g.request_id FOR SHARE)
        FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='engine_access_grant_relation_exists';
    END IF;
END
