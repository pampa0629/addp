CREATE TRIGGER IF NOT EXISTS source_grant_no_update
BEFORE UPDATE ON engine_access_grants FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='engine_access_grants is immutable'
