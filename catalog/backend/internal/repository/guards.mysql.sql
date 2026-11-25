CREATE TRIGGER IF NOT EXISTS require_current_sharing_basis
BEFORE INSERT ON sharing_decisions FOR EACH ROW
BEGIN
    DECLARE target_version BIGINT;
    SELECT version INTO target_version FROM entries
     WHERE tenant_id=NEW.tenant_id AND id=NEW.catalog_entry_id AND entry_type='data_item'
       AND entry_status='active' AND governance_status<>'deprecated' FOR UPDATE;
    IF target_version IS NULL OR NEW.entry_version<>target_version+1 OR NEW.created_at>SYSDATE(6)
       OR (NEW.expiry_mode='at_time' AND NEW.expires_at<=SYSDATE(6)) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='sharing decision requires a current eligible entry';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM source_bindings WHERE tenant_id=NEW.tenant_id AND catalog_entry_id=NEW.catalog_entry_id
        AND id=NEW.source_binding_id AND is_current AND source_status='active' AND source_module='meta'
        AND source_type='data_item' AND source_identity=NEW.item_fingerprint AND source_version=NEW.source_version FOR SHARE)
       OR NOT EXISTS (SELECT 1 FROM responsibilities WHERE tenant_id=NEW.tenant_id AND catalog_entry_id=NEW.catalog_entry_id
        AND id=NEW.responsibility_id AND role='business_owner' AND subject_type='user' AND subject_id=NEW.confirmed_by AND status='active' FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='sharing decision requires current source and business ownership';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS preserve_sharing_update
BEFORE UPDATE ON sharing_decisions FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='sharing_decisions history is immutable'$$

CREATE TRIGGER IF NOT EXISTS preserve_sharing_delete
BEFORE DELETE ON sharing_decisions FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='sharing_decisions history is immutable'$$

CREATE TRIGGER IF NOT EXISTS require_current_fulfillment_basis
BEFORE INSERT ON fulfillment_checks FOR EACH ROW
BEGIN
    DECLARE eligible CHAR(36);
    SELECT id INTO eligible FROM entries WHERE tenant_id=NEW.tenant_id AND id=NEW.catalog_entry_id
      AND entry_type='data_item' AND entry_status='active' AND governance_status<>'deprecated' FOR UPDATE;
    IF eligible IS NULL OR NEW.resolved_at IS NOT NULL OR NEW.grant_reconciled_at IS NOT NULL
       OR NOT EXISTS (SELECT 1 FROM source_bindings WHERE tenant_id=NEW.tenant_id AND catalog_entry_id=NEW.catalog_entry_id
                      AND is_current AND source_status='active' AND source_module='meta' FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fulfillment check requires a current non-deprecated data item';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS preserve_fulfillment_binding
BEFORE UPDATE ON fulfillment_checks FOR EACH ROW
BEGIN
    DECLARE locked_entry CHAR(36);
    IF NOT (NEW.request_id=OLD.request_id AND NEW.tenant_id=OLD.tenant_id AND NEW.catalog_entry_id=OLD.catalog_entry_id
            AND NEW.request_binding=OLD.request_binding AND NEW.created_at=OLD.created_at
            AND ((OLD.resolved_at IS NULL AND NEW.resolved_at IS NOT NULL AND NEW.grant_reconciled_at IS NULL)
              OR (OLD.resolved_at IS NOT NULL AND NEW.resolved_at<=>OLD.resolved_at
                 AND OLD.grant_reconciled_at IS NULL AND NEW.grant_reconciled_at IS NOT NULL))) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fulfillment check binding is immutable';
    END IF;
    SELECT id INTO locked_entry FROM entries WHERE id=NEW.catalog_entry_id AND tenant_id=NEW.tenant_id FOR UPDATE;
END$$

CREATE TRIGGER IF NOT EXISTS preserve_fulfillment_delete
BEFORE DELETE ON fulfillment_checks FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fulfillment_checks history is immutable'$$

CREATE TRIGGER IF NOT EXISTS guard_entries_update
BEFORE UPDATE ON entries FOR EACH ROW
guard: BEGIN
    IF (OLD.tenant_id <=> NEW.tenant_id) AND
       (OLD.id <=> NEW.id) AND
       (OLD.entry_type <=> NEW.entry_type) AND
       (OLD.entry_status <=> NEW.entry_status) AND
       (OLD.governance_status <=> NEW.governance_status) AND
       (OLD.merged_into_entry_id <=> NEW.merged_into_entry_id) THEN LEAVE guard; END IF;
    IF EXISTS (SELECT 1 FROM fulfillment_checks WHERE resolved_at IS NULL
        AND ((tenant_id=OLD.tenant_id AND catalog_entry_id=OLD.id)) FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='catalog_fulfillment_unresolved';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS guard_entries_delete
BEFORE DELETE ON entries FOR EACH ROW
guard: BEGIN
    IF EXISTS (SELECT 1 FROM fulfillment_checks WHERE resolved_at IS NULL
        AND ((tenant_id=OLD.tenant_id AND catalog_entry_id=OLD.id)) FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='catalog_fulfillment_unresolved';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS guard_source_bindings_insert
BEFORE INSERT ON source_bindings FOR EACH ROW
guard: BEGIN
    DECLARE locked_entry CHAR(36);
    SELECT id INTO locked_entry FROM entries WHERE id=NEW.catalog_entry_id AND tenant_id=NEW.tenant_id FOR UPDATE;
    IF EXISTS (SELECT 1 FROM fulfillment_checks WHERE resolved_at IS NULL
        AND ((tenant_id=NEW.tenant_id AND catalog_entry_id=NEW.catalog_entry_id)) FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='catalog_fulfillment_unresolved';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS guard_source_bindings_update
BEFORE UPDATE ON source_bindings FOR EACH ROW
guard: BEGIN
    DECLARE locked_entry CHAR(36);
    IF (OLD.tenant_id <=> NEW.tenant_id) AND
       (OLD.catalog_entry_id <=> NEW.catalog_entry_id) AND
       (OLD.source_module <=> NEW.source_module) AND
       (OLD.source_type <=> NEW.source_type) AND
       (OLD.source_identity <=> NEW.source_identity) AND
       (OLD.source_status <=> NEW.source_status) AND
       (OLD.is_current <=> NEW.is_current) AND
       (OLD.source_version <=> NEW.source_version) AND
       (OLD.observed_snapshot <=> NEW.observed_snapshot) THEN LEAVE guard; END IF;
    IF NEW.catalog_entry_id < OLD.catalog_entry_id THEN
        SELECT id INTO locked_entry FROM entries WHERE id=NEW.catalog_entry_id AND tenant_id=NEW.tenant_id FOR UPDATE;
        SELECT id INTO locked_entry FROM entries WHERE id=OLD.catalog_entry_id AND tenant_id=OLD.tenant_id FOR UPDATE;
    ELSE
        SELECT id INTO locked_entry FROM entries WHERE id=OLD.catalog_entry_id AND tenant_id=OLD.tenant_id FOR UPDATE;
        IF NEW.catalog_entry_id<>OLD.catalog_entry_id OR NEW.tenant_id<>OLD.tenant_id THEN
            SELECT id INTO locked_entry FROM entries WHERE id=NEW.catalog_entry_id AND tenant_id=NEW.tenant_id FOR UPDATE;
        END IF;
    END IF;
    IF EXISTS (SELECT 1 FROM fulfillment_checks WHERE resolved_at IS NULL
        AND ((tenant_id=OLD.tenant_id AND catalog_entry_id=OLD.catalog_entry_id) OR (tenant_id=NEW.tenant_id AND catalog_entry_id=NEW.catalog_entry_id)) FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='catalog_fulfillment_unresolved';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS guard_source_bindings_delete
BEFORE DELETE ON source_bindings FOR EACH ROW
guard: BEGIN
    DECLARE locked_entry CHAR(36);
    SELECT id INTO locked_entry FROM entries WHERE id=OLD.catalog_entry_id AND tenant_id=OLD.tenant_id FOR UPDATE;
    IF EXISTS (SELECT 1 FROM fulfillment_checks WHERE resolved_at IS NULL
        AND ((tenant_id=OLD.tenant_id AND catalog_entry_id=OLD.catalog_entry_id)) FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='catalog_fulfillment_unresolved';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS guard_responsibilities_insert
BEFORE INSERT ON responsibilities FOR EACH ROW
guard: BEGIN
    DECLARE locked_entry CHAR(36);
    SELECT id INTO locked_entry FROM entries WHERE id=NEW.catalog_entry_id AND tenant_id=NEW.tenant_id FOR UPDATE;
    IF EXISTS (SELECT 1 FROM fulfillment_checks WHERE resolved_at IS NULL
        AND ((tenant_id=NEW.tenant_id AND catalog_entry_id=NEW.catalog_entry_id)) FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='catalog_fulfillment_unresolved';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS guard_responsibilities_update
BEFORE UPDATE ON responsibilities FOR EACH ROW
guard: BEGIN
    DECLARE locked_entry CHAR(36);
    IF (OLD.tenant_id <=> NEW.tenant_id) AND
       (OLD.catalog_entry_id <=> NEW.catalog_entry_id) AND
       (OLD.role <=> NEW.role) AND
       (OLD.subject_type <=> NEW.subject_type) AND
       (OLD.subject_id <=> NEW.subject_id) AND
       (OLD.status <=> NEW.status) THEN LEAVE guard; END IF;
    IF NEW.catalog_entry_id < OLD.catalog_entry_id THEN
        SELECT id INTO locked_entry FROM entries WHERE id=NEW.catalog_entry_id AND tenant_id=NEW.tenant_id FOR UPDATE;
        SELECT id INTO locked_entry FROM entries WHERE id=OLD.catalog_entry_id AND tenant_id=OLD.tenant_id FOR UPDATE;
    ELSE
        SELECT id INTO locked_entry FROM entries WHERE id=OLD.catalog_entry_id AND tenant_id=OLD.tenant_id FOR UPDATE;
        IF NEW.catalog_entry_id<>OLD.catalog_entry_id OR NEW.tenant_id<>OLD.tenant_id THEN
            SELECT id INTO locked_entry FROM entries WHERE id=NEW.catalog_entry_id AND tenant_id=NEW.tenant_id FOR UPDATE;
        END IF;
    END IF;
    IF EXISTS (SELECT 1 FROM fulfillment_checks WHERE resolved_at IS NULL
        AND ((tenant_id=OLD.tenant_id AND catalog_entry_id=OLD.catalog_entry_id) OR (tenant_id=NEW.tenant_id AND catalog_entry_id=NEW.catalog_entry_id)) FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='catalog_fulfillment_unresolved';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS guard_responsibilities_delete
BEFORE DELETE ON responsibilities FOR EACH ROW
guard: BEGIN
    DECLARE locked_entry CHAR(36);
    SELECT id INTO locked_entry FROM entries WHERE id=OLD.catalog_entry_id AND tenant_id=OLD.tenant_id FOR UPDATE;
    IF EXISTS (SELECT 1 FROM fulfillment_checks WHERE resolved_at IS NULL
        AND ((tenant_id=OLD.tenant_id AND catalog_entry_id=OLD.catalog_entry_id)) FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='catalog_fulfillment_unresolved';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS preserve_responsibility_establishment
BEFORE UPDATE ON entries FOR EACH ROW
BEGIN
    IF NOT (NEW.business_responsibility_established <=> OLD.business_responsibility_established)
       AND NOT (NEW.business_responsibility_established <=> TRUE) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='business responsibility establishment evidence cannot be reset';
    END IF;
END$$
