CREATE TRIGGER IF NOT EXISTS trg_context_selection_options_before_insert
BEFORE INSERT ON context_selection_ticket_options FOR EACH ROW
BEGIN
    DECLARE subject_id BIGINT DEFAULT NULL;
    DECLARE assurance VARCHAR(16);
    SELECT principal_id, assurance_level INTO subject_id, assurance FROM context_selection_tickets
     WHERE id=NEW.ticket_id AND consumed_at IS NULL AND expires_at > CURRENT_TIMESTAMP(6) FOR UPDATE;
    IF subject_id IS NULL THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'context selection ticket is not pending';
    END IF;
    IF NEW.context_type='tenant' AND NOT EXISTS (
        SELECT 1 FROM tenant_memberships m JOIN tenants t ON t.id=m.tenant_id
         WHERE m.id=NEW.tenant_membership_id AND m.principal_id=subject_id AND m.status='active' AND t.status='active'
           AND (m.expires_at IS NULL OR m.expires_at > CURRENT_TIMESTAMP(6)) FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'tenant option requires an active membership for the ticket principal';
    END IF;
    IF NEW.context_type='platform' AND (assurance NOT IN ('aal2','aal3') OR NOT EXISTS (
        SELECT 1 FROM role_assignments WHERE principal_id=subject_id AND scope_type='platform' AND status='active'
           AND valid_from <= CURRENT_TIMESTAMP(6) AND (valid_until IS NULL OR valid_until > CURRENT_TIMESTAMP(6)) FOR SHARE
    )) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'platform option requires an effective platform role and assurance';
    END IF;
END
