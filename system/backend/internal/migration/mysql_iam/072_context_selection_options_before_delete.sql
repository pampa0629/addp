CREATE TRIGGER IF NOT EXISTS trg_context_selection_options_before_delete
BEFORE DELETE ON context_selection_ticket_options FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'context selection option cannot be deleted'
