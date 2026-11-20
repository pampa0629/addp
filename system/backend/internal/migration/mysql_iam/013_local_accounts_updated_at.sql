CREATE TRIGGER IF NOT EXISTS trg_local_accounts_updated_at
BEFORE UPDATE ON local_accounts FOR EACH ROW
SET NEW.updated_at = CURRENT_TIMESTAMP(6)
