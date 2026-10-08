CREATE TABLE ontology.platform_projections (
    generation UUID PRIMARY KEY CHECK (generation <> '00000000-0000-0000-0000-000000000000'),
    capability TEXT NOT NULL,
    revision BIGINT NOT NULL,
    digest TEXT NOT NULL,
    baseline_activation_version BIGINT NOT NULL CHECK (baseline_activation_version >= 0),
    status TEXT NOT NULL CHECK (status IN ('building', 'ready', 'failed', 'superseded')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (capability, generation),
    UNIQUE (capability, revision, generation),
    FOREIGN KEY (capability, revision, digest)
        REFERENCES ontology.platform_revisions(capability, revision, digest)
);

ALTER TABLE ontology.platform_capabilities
    ADD COLUMN publish_generation UUID,
    ADD COLUMN active_revision BIGINT,
    ADD COLUMN active_generation UUID,
    ADD COLUMN activation_version BIGINT NOT NULL DEFAULT 0 CHECK (activation_version >= 0),
    ADD CONSTRAINT platform_active_pair CHECK (
        (active_revision IS NULL AND active_generation IS NULL AND activation_version = 0) OR
        (active_revision IS NOT NULL AND active_generation IS NOT NULL AND activation_version > 0)
    ),
    ADD CONSTRAINT platform_active_revision_order CHECK (active_revision <= last_revision),
    ADD CONSTRAINT platform_publish_reference FOREIGN KEY (capability, publish_generation)
        REFERENCES ontology.platform_projections(capability, generation),
    ADD CONSTRAINT platform_active_reference FOREIGN KEY (capability, active_revision, active_generation)
        REFERENCES ontology.platform_projections(capability, revision, generation);

CREATE TABLE ontology.platform_projection_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    generation UUID NOT NULL REFERENCES ontology.platform_projections(generation),
    action TEXT NOT NULL CHECK (action IN ('begin', 'ready', 'failed', 'superseded')),
    actor_principal_id BIGINT NOT NULL CHECK (actor_principal_id > 0),
    authorization_version BIGINT NOT NULL CHECK (authorization_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (generation, action)
);

CREATE TRIGGER platform_projection_event_immutable
BEFORE UPDATE OR DELETE ON ontology.platform_projection_events
FOR EACH ROW EXECUTE FUNCTION ontology.guard_platform_record_immutable();

CREATE FUNCTION ontology.guard_platform_projection() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'platform projection history is immutable' USING ERRCODE = '23514';
    END IF;
    IF (NEW.generation, NEW.capability, NEW.revision, NEW.digest,
        NEW.baseline_activation_version, NEW.created_at) IS DISTINCT FROM
       (OLD.generation, OLD.capability, OLD.revision, OLD.digest,
        OLD.baseline_activation_version, OLD.created_at)
       OR OLD.status <> 'building' OR NEW.status NOT IN ('ready', 'failed', 'superseded') THEN
        RAISE EXCEPTION 'invalid platform projection transition' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER platform_projection_guard
BEFORE UPDATE OR DELETE ON ontology.platform_projections
FOR EACH ROW EXECUTE FUNCTION ontology.guard_platform_projection();

CREATE FUNCTION ontology.guard_platform_capability() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.capability <> OLD.capability OR NEW.last_revision < OLD.last_revision THEN
        RAISE EXCEPTION 'platform identity or revision rollback' USING ERRCODE = '23514';
    END IF;
    IF (NEW.active_revision, NEW.active_generation) IS DISTINCT FROM
       (OLD.active_revision, OLD.active_generation) THEN
        IF NEW.activation_version <> OLD.activation_version + 1 OR
           NEW.active_generation IS DISTINCT FROM OLD.publish_generation OR
           NEW.publish_generation IS DISTINCT FROM OLD.publish_generation OR
           NOT EXISTS (SELECT 1 FROM ontology.platform_projections p
                       WHERE p.capability = NEW.capability AND p.generation = NEW.active_generation
                         AND p.revision = NEW.active_revision AND p.revision = NEW.last_revision
                         AND p.status = 'ready' AND p.baseline_activation_version = OLD.activation_version) THEN
            RAISE EXCEPTION 'invalid platform activation fence' USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.activation_version <> OLD.activation_version THEN
        RAISE EXCEPTION 'activation version changed without activation' USING ERRCODE = '23514';
    END IF;
    IF NEW.publish_generation IS DISTINCT FROM OLD.publish_generation AND
       NOT EXISTS (SELECT 1 FROM ontology.platform_projections p
                   WHERE p.capability = NEW.capability AND p.generation = NEW.publish_generation
                     AND p.revision = NEW.last_revision AND p.status = 'building'
                     AND p.baseline_activation_version = OLD.activation_version) THEN
        RAISE EXCEPTION 'invalid platform publication fence' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER platform_capability_guard
BEFORE UPDATE ON ontology.platform_capabilities
FOR EACH ROW EXECUTE FUNCTION ontology.guard_platform_capability();
