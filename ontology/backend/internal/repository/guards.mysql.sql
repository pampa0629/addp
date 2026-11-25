CREATE TRIGGER IF NOT EXISTS guard_activation_update
BEFORE UPDATE ON ontologies FOR EACH ROW
BEGIN
    IF NOT (NEW.active_revision <=> OLD.active_revision AND NEW.active_generation <=> OLD.active_generation) THEN
        IF NEW.activation_version <> OLD.activation_version+1 THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'activation version must advance';
        END IF;
        IF NEW.active_generation IS NOT NULL AND NOT EXISTS (
            SELECT 1 FROM projections p JOIN revisions r
              ON (r.tenant_id,r.ontology_id,r.revision)=(p.tenant_id,p.ontology_id,p.revision)
            WHERE p.generation=NEW.active_generation AND p.tenant_id=NEW.tenant_id
              AND p.ontology_id=NEW.ontology_id AND p.revision=NEW.active_revision
              AND p.status='ready' AND r.status='published' AND p.baseline_version=OLD.activation_version
        ) THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'activation requires ready published projection and exact baseline';
        END IF;
    ELSEIF NEW.activation_version <> OLD.activation_version THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'activation version cannot advance without pointer change';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS guard_projection_insert
BEFORE INSERT ON projections FOR EACH ROW
BEGIN
    IF NEW.status <> 'pending' OR NOT EXISTS (
        SELECT 1 FROM revisions r WHERE r.tenant_id=NEW.tenant_id AND r.ontology_id=NEW.ontology_id
        AND r.revision=NEW.revision AND r.status='published' AND r.digest=NEW.digest
    ) OR NOT EXISTS (
        SELECT 1 FROM ontologies h WHERE h.tenant_id=NEW.tenant_id AND h.ontology_id=NEW.ontology_id
        AND h.activation_version=NEW.baseline_version
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'projection requires exact publication and baseline';
    END IF;
    IF NEW.predecessor_generation IS NULL THEN
        IF NOT EXISTS (SELECT 1 FROM revisions r WHERE r.tenant_id=NEW.tenant_id
            AND r.ontology_id=NEW.ontology_id AND r.revision=NEW.revision
            AND r.generation=NEW.generation AND r.build_execution_id=NEW.execution_id) THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'initial projection requires publication identity';
        END IF;
    ELSEIF NOT EXISTS (SELECT 1 FROM projections p WHERE p.generation=NEW.predecessor_generation
        AND p.tenant_id=NEW.tenant_id AND p.ontology_id=NEW.ontology_id AND p.revision=NEW.revision
        AND p.digest=NEW.digest AND p.status='failed') THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'rebuild requires failed predecessor';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS guard_projection_delete
BEFORE DELETE ON projections FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'projection deletion requires owner retention policy';
END$$

CREATE TRIGGER IF NOT EXISTS guard_projection_update
BEFORE UPDATE ON projections FOR EACH ROW
BEGIN
    IF NOT (NEW.generation <=> OLD.generation AND NEW.tenant_id <=> OLD.tenant_id AND NEW.ontology_id <=> OLD.ontology_id AND NEW.revision <=> OLD.revision AND NEW.digest <=> OLD.digest AND NEW.execution_id <=> OLD.execution_id AND NEW.baseline_version <=> OLD.baseline_version AND NEW.created_at <=> OLD.created_at AND NEW.predecessor_generation <=> OLD.predecessor_generation)
        OR NOT ((OLD.status='pending' AND NEW.status IN ('building','failed'))
            OR (OLD.status='building' AND NEW.status IN ('ready','failed'))) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'projection identity or transition violation';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS guard_projection_event_delete
BEFORE DELETE ON projection_events FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'revision events are append only';
END$$

CREATE TRIGGER IF NOT EXISTS guard_projection_event_update
BEFORE UPDATE ON projection_events FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'revision events are append only';
END$$

CREATE TRIGGER IF NOT EXISTS guard_revision_insert
BEFORE INSERT ON revisions FOR EACH ROW
BEGIN
    IF NEW.status <> 'draft' OR NEW.version <> 1 THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'revision must start as draft version 1';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS guard_revision_delete
BEFORE DELETE ON revisions FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'ontology revision deletion is not supported';
END$$

CREATE TRIGGER IF NOT EXISTS guard_revision_update
BEFORE UPDATE ON revisions FOR EACH ROW
BEGIN
    IF NOT (NEW.tenant_id <=> OLD.tenant_id AND NEW.ontology_id <=> OLD.ontology_id AND NEW.revision <=> OLD.revision AND NEW.created_at <=> OLD.created_at)
        OR NEW.version <> OLD.version + 1 THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'revision identity or version violation';
    END IF;
    IF NOT ((OLD.status = 'draft' AND NEW.status IN ('draft','in_review'))
        OR (OLD.status = 'in_review' AND NEW.status IN ('draft','published'))
        OR (OLD.status = 'published' AND NEW.status = 'withdrawn')) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'invalid revision transition';
    END IF;
    IF (OLD.status <> 'draft' OR NEW.status <> 'draft') AND
        NOT (NEW.payload <=> OLD.payload AND NEW.digest <=> OLD.digest) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reviewed or published content is immutable';
    END IF;
    IF OLD.status = 'published' AND NOT (NEW.build_execution_id <=> OLD.build_execution_id AND NEW.generation <=> OLD.generation AND NEW.published_at <=> OLD.published_at) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'publication identity is immutable';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS guard_revision_event_delete
BEFORE DELETE ON revision_events FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'revision events are append only';
END$$

CREATE TRIGGER IF NOT EXISTS guard_revision_event_update
BEFORE UPDATE ON revision_events FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'revision events are append only';
END$$

CREATE TRIGGER IF NOT EXISTS platform_capability_guard_update
BEFORE UPDATE ON platform_capabilities FOR EACH ROW
BEGIN
    IF NEW.capability <> OLD.capability OR NEW.last_revision < OLD.last_revision THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'platform identity or revision rollback';
    END IF;
    IF NOT (NEW.active_revision <=> OLD.active_revision AND NEW.active_generation <=> OLD.active_generation) THEN
        IF NEW.activation_version <> OLD.activation_version + 1 OR
           NOT (NEW.active_generation <=> OLD.publish_generation) OR
           NOT (NEW.publish_generation <=> OLD.publish_generation) OR
           NOT EXISTS (SELECT 1 FROM platform_projections p
                       WHERE p.capability = NEW.capability AND p.generation = NEW.active_generation
                         AND p.revision = NEW.active_revision AND p.revision = NEW.last_revision
                         AND p.status = 'ready' AND p.baseline_activation_version = OLD.activation_version) THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'invalid platform activation fence';
        END IF;
    ELSEIF NEW.activation_version <> OLD.activation_version THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'activation version changed without activation';
    END IF;
    IF NOT (NEW.publish_generation <=> OLD.publish_generation) AND
       NOT EXISTS (SELECT 1 FROM platform_projections p
                   WHERE p.capability = NEW.capability AND p.generation = NEW.publish_generation
                     AND p.revision = NEW.last_revision AND p.status = 'building'
                     AND p.baseline_activation_version = OLD.activation_version) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'invalid platform publication fence';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS platform_projection_event_immutable_delete
BEFORE DELETE ON platform_projection_events FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'platform revision and audit records are immutable';
END$$

CREATE TRIGGER IF NOT EXISTS platform_projection_event_immutable_update
BEFORE UPDATE ON platform_projection_events FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'platform revision and audit records are immutable';
END$$

CREATE TRIGGER IF NOT EXISTS platform_projection_guard_delete
BEFORE DELETE ON platform_projections FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'platform projection history is immutable';
END$$

CREATE TRIGGER IF NOT EXISTS platform_projection_guard_update
BEFORE UPDATE ON platform_projections FOR EACH ROW
BEGIN
    IF NOT (NEW.generation <=> OLD.generation AND NEW.capability <=> OLD.capability AND NEW.revision <=> OLD.revision AND NEW.digest <=> OLD.digest AND NEW.baseline_activation_version <=> OLD.baseline_activation_version AND NEW.created_at <=> OLD.created_at)
       OR OLD.status <> 'building' OR NEW.status NOT IN ('ready', 'failed', 'superseded') THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'invalid platform projection transition';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS platform_revision_event_immutable_delete
BEFORE DELETE ON platform_revision_events FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'platform revision and audit records are immutable';
END$$

CREATE TRIGGER IF NOT EXISTS platform_revision_event_immutable_update
BEFORE UPDATE ON platform_revision_events FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'platform revision and audit records are immutable';
END$$

CREATE TRIGGER IF NOT EXISTS platform_revision_immutable_delete
BEFORE DELETE ON platform_revisions FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'platform revision and audit records are immutable';
END$$

CREATE TRIGGER IF NOT EXISTS platform_revision_immutable_update
BEFORE UPDATE ON platform_revisions FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'platform revision and audit records are immutable';
END$$

CREATE TRIGGER IF NOT EXISTS revision_events_event_identity
BEFORE INSERT ON revision_events FOR EACH ROW
BEGIN
    IF NEW.id <> 0 THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'event id is assigned by the database';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS projection_events_event_identity
BEFORE INSERT ON projection_events FOR EACH ROW
BEGIN
    IF NEW.id <> 0 THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'event id is assigned by the database';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS platform_revision_events_event_identity
BEFORE INSERT ON platform_revision_events FOR EACH ROW
BEGIN
    IF NEW.id <> 0 THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'event id is assigned by the database';
    END IF;
END$$

CREATE TRIGGER IF NOT EXISTS platform_projection_events_event_identity
BEFORE INSERT ON platform_projection_events FOR EACH ROW
BEGIN
    IF NEW.id <> 0 THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'event id is assigned by the database';
    END IF;
END$$
