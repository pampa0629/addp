CREATE TRIGGER IF NOT EXISTS source_upload_result_no_update
BEFORE UPDATE ON engine_access_upload_grant_outcomes FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='engine_access_upload_grant_outcomes is immutable'
