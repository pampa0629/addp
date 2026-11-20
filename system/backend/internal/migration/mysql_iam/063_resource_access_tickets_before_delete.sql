CREATE TRIGGER IF NOT EXISTS trg_resource_access_tickets_before_delete
BEFORE DELETE ON resource_access_tickets FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'resource ticket history cannot be deleted'
