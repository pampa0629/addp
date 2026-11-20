CREATE TRIGGER IF NOT EXISTS trg_context_selection_tickets_before_update
BEFORE UPDATE ON context_selection_tickets FOR EACH ROW
BEGIN
    IF NEW.id <> OLD.id OR NEW.token_hash <> OLD.token_hash OR NEW.principal_id <> OLD.principal_id
       OR NEW.issued_authorization_version <> OLD.issued_authorization_version OR NEW.client_id <> OLD.client_id
       OR NEW.authentication_methods <> OLD.authentication_methods OR NEW.assurance_level <> OLD.assurance_level
       OR NEW.authenticated_at <> OLD.authenticated_at OR NOT (NEW.step_up_expires_at <=> OLD.step_up_expires_at)
       OR NEW.expires_at <> OLD.expires_at OR NEW.created_at <> OLD.created_at
       OR OLD.consumed_at IS NOT NULL OR NEW.consumed_at IS NULL THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'context selection ticket may only be consumed once without changing its binding';
    END IF;
END
