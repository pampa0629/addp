CREATE TRIGGER IF NOT EXISTS trg_role_assignments_before_update
BEFORE UPDATE ON role_assignments FOR EACH ROW
BEGIN
    IF OLD.status <> 'active' OR NEW.status <> 'revoked'
       OR NEW.principal_id <> OLD.principal_id OR NEW.role_id <> OLD.role_id
       OR NEW.scope_type <> OLD.scope_type OR NOT (NEW.tenant_id <=> OLD.tenant_id)
       OR NEW.source_type <> OLD.source_type OR NOT (NEW.created_by_principal_id <=> OLD.created_by_principal_id)
       OR NEW.valid_from <> OLD.valid_from OR NOT (NEW.valid_until <=> OLD.valid_until)
       OR NOT (NEW.grant_reason <=> OLD.grant_reason) OR NEW.created_at <> OLD.created_at THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'role assignment identity cannot change and the only transition is revocation';
    END IF;
    IF OLD.valid_until IS NOT NULL AND OLD.valid_until <= CURRENT_TIMESTAMP(6) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'expired role assignment is read-only';
    END IF;
    IF OLD.scope_type = 'platform' THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'platform revocation requires the privileged request migration';
    END IF;
    SET NEW.updated_at = CURRENT_TIMESTAMP(6);
END
