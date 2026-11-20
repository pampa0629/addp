CREATE TRIGGER IF NOT EXISTS trg_context_selection_options_before_update
BEFORE UPDATE ON context_selection_ticket_options FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'context selection option is immutable'
