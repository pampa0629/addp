-- Apply after reviewing the installed System tables, guards and procedure.
GRANT INSERT ON system.engine_access_grants TO 'addp_system_source_grant'@'localhost';
GRANT INSERT ON system.engine_access_initial_grant_outcomes TO 'addp_system_source_grant'@'localhost';
GRANT INSERT ON system.engine_access_upload_grant_outcomes TO 'addp_system_source_grant'@'localhost';
GRANT EXECUTE ON PROCEDURE system.persist_source_grant TO 'addp_system_source_grant'@'localhost';
