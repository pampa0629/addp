ALTER TABLE ontology.ontologies
    ADD COLUMN activation_version BIGINT NOT NULL DEFAULT 1 CHECK (activation_version > 0),
    ADD COLUMN active_revision BIGINT,
    ADD COLUMN active_generation UUID,
    ADD CONSTRAINT active_pair CHECK (num_nonnulls(active_revision,active_generation) IN (0,2));

CREATE TABLE ontology.projections (
    generation UUID PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    ontology_id VARCHAR(64) NOT NULL,
    revision BIGINT NOT NULL,
    digest VARCHAR(64) NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
    execution_id UUID NOT NULL UNIQUE,
    baseline_version BIGINT NOT NULL CHECK (baseline_version > 0),
    status VARCHAR(16) NOT NULL CHECK (status IN ('pending','building','ready','failed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (tenant_id,ontology_id,revision,generation),
    FOREIGN KEY (tenant_id,ontology_id,revision) REFERENCES ontology.revisions(tenant_id,ontology_id,revision)
);
CREATE INDEX projection_revision ON ontology.projections(tenant_id,ontology_id,revision);
ALTER TABLE ontology.ontologies ADD CONSTRAINT active_projection
    FOREIGN KEY (tenant_id,ontology_id,active_revision,active_generation)
    REFERENCES ontology.projections(tenant_id,ontology_id,revision,generation);

CREATE TABLE ontology.projection_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    generation UUID NOT NULL REFERENCES ontology.projections(generation),
    action VARCHAR(16) NOT NULL CHECK (action IN ('building','activated','failed')),
    attempt INTEGER NOT NULL CHECK (attempt >= 0),
    actor_principal_id BIGINT NOT NULL CHECK (actor_principal_id > 0),
    actor_membership_id BIGINT NOT NULL CHECK (actor_membership_id > 0),
    authorization_version BIGINT NOT NULL CHECK (authorization_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE(generation,action)
);
CREATE TRIGGER guard_projection_event BEFORE UPDATE OR DELETE ON ontology.projection_events
    FOR EACH ROW EXECUTE FUNCTION ontology.guard_revision_event();

CREATE FUNCTION ontology.guard_projection() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'projection deletion requires owner retention policy' USING ERRCODE='23514';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'pending' OR NOT EXISTS (
            SELECT 1 FROM ontology.revisions r WHERE r.tenant_id=NEW.tenant_id AND r.ontology_id=NEW.ontology_id
            AND r.revision=NEW.revision AND r.status='published' AND r.generation=NEW.generation
            AND r.build_execution_id=NEW.execution_id AND r.digest=NEW.digest
        ) THEN
            RAISE EXCEPTION 'projection requires exact publication' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF (NEW.generation,NEW.tenant_id,NEW.ontology_id,NEW.revision,NEW.digest,NEW.execution_id,NEW.baseline_version,NEW.created_at)
        IS DISTINCT FROM (OLD.generation,OLD.tenant_id,OLD.ontology_id,OLD.revision,OLD.digest,OLD.execution_id,OLD.baseline_version,OLD.created_at)
        OR NOT ((OLD.status='pending' AND NEW.status IN ('building','failed'))
            OR (OLD.status='building' AND NEW.status IN ('ready','failed'))) THEN
        RAISE EXCEPTION 'projection identity or transition violation' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_projection BEFORE INSERT OR UPDATE OR DELETE ON ontology.projections
    FOR EACH ROW EXECUTE FUNCTION ontology.guard_projection();

CREATE FUNCTION ontology.guard_activation() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.active_revision,NEW.active_generation) IS DISTINCT FROM (OLD.active_revision,OLD.active_generation) THEN
        IF NEW.activation_version <> OLD.activation_version+1 THEN
            RAISE EXCEPTION 'activation version must advance' USING ERRCODE='23514';
        END IF;
        IF NEW.active_generation IS NOT NULL AND NOT EXISTS (
            SELECT 1 FROM ontology.projections p JOIN ontology.revisions r
              ON (r.tenant_id,r.ontology_id,r.revision)=(p.tenant_id,p.ontology_id,p.revision)
            WHERE p.generation=NEW.active_generation AND p.tenant_id=NEW.tenant_id
              AND p.ontology_id=NEW.ontology_id AND p.revision=NEW.active_revision
              AND p.status='ready' AND r.status='published' AND p.baseline_version=OLD.activation_version
        ) THEN
            RAISE EXCEPTION 'activation requires ready published projection and exact baseline' USING ERRCODE='23514';
        END IF;
    ELSIF NEW.activation_version <> OLD.activation_version THEN
        RAISE EXCEPTION 'activation version cannot advance without pointer change' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_activation BEFORE UPDATE ON ontology.ontologies
    FOR EACH ROW EXECUTE FUNCTION ontology.guard_activation();
