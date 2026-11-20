CREATE TRIGGER IF NOT EXISTS trg_context_selection_tickets_before_delete
BEFORE DELETE ON context_selection_tickets FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'context selection history cannot be deleted'
