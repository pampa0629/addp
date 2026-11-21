CREATE TRIGGER IF NOT EXISTS source_receipt_expiry
BEFORE INSERT ON engine_access_fulfillment_outcomes FOR EACH ROW
BEGIN
    IF NEW.expiry_mode='at_time' AND NOT (NEW.grant_expires_at <=>
        CAST(REPLACE(JSON_UNQUOTE(JSON_EXTRACT(NEW.binding,'$.expires_at')),'Z','+00:00') AS DATETIME(6))) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='source receipt expiry differs from binding';
    END IF;
END
