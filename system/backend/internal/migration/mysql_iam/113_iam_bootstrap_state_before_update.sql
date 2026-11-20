CREATE TRIGGER IF NOT EXISTS trg_iam_bootstrap_state_before_update
BEFORE UPDATE ON iam_bootstrap_state FOR EACH ROW
BEGIN
    IF NEW.singleton <> OLD.singleton OR NEW.prepared_at <> OLD.prepared_at OR NEW.expires_at <> OLD.expires_at
       OR OLD.status <> 'prepared' OR NEW.status <> 'completed' OR NEW.secret_hash IS NOT NULL OR NEW.completed_at IS NULL THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'IAM bootstrap may only complete its original preparation';
    END IF;
END
