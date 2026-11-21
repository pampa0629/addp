CREATE TRIGGER IF NOT EXISTS source_receipt_no_delete
BEFORE DELETE ON engine_access_fulfillment_outcomes FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='engine_access_fulfillment_outcomes is immutable'
