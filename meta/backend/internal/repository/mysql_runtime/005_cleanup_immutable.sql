CREATE TRIGGER IF NOT EXISTS cleanup_result_immutable
BEFORE UPDATE ON cleanup_results FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='cleanup result is immutable'
