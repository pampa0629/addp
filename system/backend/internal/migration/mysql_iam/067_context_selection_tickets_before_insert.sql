CREATE TRIGGER IF NOT EXISTS trg_context_selection_tickets_before_insert
BEFORE INSERT ON context_selection_tickets FOR EACH ROW
BEGIN
    IF NEW.consumed_at IS NOT NULL OR NOT EXISTS (
        SELECT 1 FROM principals WHERE id=NEW.principal_id AND principal_type='user' AND status='active'
           AND authorization_version=NEW.issued_authorization_version FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'context selection requires an active user at the issued authorization version';
    END IF;
END
