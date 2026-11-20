CREATE TRIGGER IF NOT EXISTS trg_iam_bootstrap_state_before_delete
BEFORE DELETE ON iam_bootstrap_state FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'IAM bootstrap state cannot be physically deleted'
