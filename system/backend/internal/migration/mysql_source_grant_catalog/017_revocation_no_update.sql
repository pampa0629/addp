CREATE TRIGGER IF NOT EXISTS source_revocation_no_update
BEFORE UPDATE ON engine_access_grant_revocations FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='engine_access_grant_revocations is immutable'
