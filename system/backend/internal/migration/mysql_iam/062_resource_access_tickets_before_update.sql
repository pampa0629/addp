CREATE TRIGGER IF NOT EXISTS trg_resource_access_tickets_before_update
BEFORE UPDATE ON resource_access_tickets FOR EACH ROW
BEGIN
    IF NEW.id <> OLD.id OR NEW.token_hash <> OLD.token_hash OR NEW.family_id <> OLD.family_id
       OR NEW.owner <> OLD.owner OR NEW.expires_at <> OLD.expires_at OR NEW.created_at <> OLD.created_at
       OR OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'resource ticket facts are immutable and revocation is one-way';
    END IF;
END
