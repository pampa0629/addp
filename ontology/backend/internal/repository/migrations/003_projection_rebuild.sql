ALTER TABLE ontology.projections ADD COLUMN predecessor_generation UUID;
ALTER TABLE ontology.projections ADD CONSTRAINT projection_predecessor
    FOREIGN KEY (tenant_id,ontology_id,revision,predecessor_generation)
    REFERENCES ontology.projections(tenant_id,ontology_id,revision,generation);
CREATE UNIQUE INDEX one_projection_successor ON ontology.projections(predecessor_generation)
    WHERE predecessor_generation IS NOT NULL;
CREATE UNIQUE INDEX one_pending_projection ON ontology.projections(tenant_id,ontology_id,revision)
    WHERE status IN ('pending','building');

ALTER TABLE ontology.projection_events DROP CONSTRAINT projection_events_action_check;
ALTER TABLE ontology.projection_events ALTER COLUMN action TYPE VARCHAR(32);
ALTER TABLE ontology.projection_events ADD CONSTRAINT projection_events_action_check
    CHECK (action IN ('rebuild_requested','building','activated','failed'));

CREATE OR REPLACE FUNCTION ontology.guard_projection() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'projection deletion requires owner retention policy' USING ERRCODE='23514';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'pending' OR NOT EXISTS (
            SELECT 1 FROM ontology.revisions r WHERE r.tenant_id=NEW.tenant_id AND r.ontology_id=NEW.ontology_id
            AND r.revision=NEW.revision AND r.status='published' AND r.digest=NEW.digest
        ) OR NOT EXISTS (
            SELECT 1 FROM ontology.ontologies h WHERE h.tenant_id=NEW.tenant_id AND h.ontology_id=NEW.ontology_id
            AND h.activation_version=NEW.baseline_version
        ) THEN
            RAISE EXCEPTION 'projection requires exact publication and baseline' USING ERRCODE='23514';
        END IF;
        IF NEW.predecessor_generation IS NULL THEN
            IF NOT EXISTS (SELECT 1 FROM ontology.revisions r WHERE r.tenant_id=NEW.tenant_id
                AND r.ontology_id=NEW.ontology_id AND r.revision=NEW.revision
                AND r.generation=NEW.generation AND r.build_execution_id=NEW.execution_id) THEN
                RAISE EXCEPTION 'initial projection requires publication identity' USING ERRCODE='23514';
            END IF;
        ELSIF NOT EXISTS (SELECT 1 FROM ontology.projections p WHERE p.generation=NEW.predecessor_generation
            AND p.tenant_id=NEW.tenant_id AND p.ontology_id=NEW.ontology_id AND p.revision=NEW.revision
            AND p.digest=NEW.digest AND p.status='failed') THEN
            RAISE EXCEPTION 'rebuild requires failed predecessor' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF (NEW.generation,NEW.tenant_id,NEW.ontology_id,NEW.revision,NEW.digest,NEW.execution_id,NEW.baseline_version,NEW.created_at,NEW.predecessor_generation)
        IS DISTINCT FROM (OLD.generation,OLD.tenant_id,OLD.ontology_id,OLD.revision,OLD.digest,OLD.execution_id,OLD.baseline_version,OLD.created_at,OLD.predecessor_generation)
        OR NOT ((OLD.status='pending' AND NEW.status IN ('building','failed'))
            OR (OLD.status='building' AND NEW.status IN ('ready','failed'))) THEN
        RAISE EXCEPTION 'projection identity or transition violation' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
