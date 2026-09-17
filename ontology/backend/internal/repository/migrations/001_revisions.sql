CREATE TABLE ontology.ontologies (
    tenant_id BIGINT NOT NULL CHECK (tenant_id > 0),
    ontology_id VARCHAR(64) NOT NULL CHECK (ontology_id ~ '^[a-z][a-z0-9_]{0,63}$'),
    last_revision BIGINT NOT NULL CHECK (last_revision >= 0),
    PRIMARY KEY (tenant_id, ontology_id)
);

CREATE TABLE ontology.revisions (
    tenant_id BIGINT NOT NULL,
    ontology_id VARCHAR(64) NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    version BIGINT NOT NULL CHECK (version > 0),
    status VARCHAR(16) NOT NULL CHECK (status IN ('draft','in_review','published','withdrawn')),
    payload TEXT NOT NULL CHECK (octet_length(payload) <= 2097152),
    digest VARCHAR(64) NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
    build_execution_id UUID UNIQUE,
    generation UUID UNIQUE,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, ontology_id, revision),
    FOREIGN KEY (tenant_id, ontology_id) REFERENCES ontology.ontologies(tenant_id, ontology_id),
    CHECK (
        (status IN ('published','withdrawn') AND num_nonnulls(build_execution_id,generation,published_at) = 3)
        OR (status IN ('draft','in_review') AND num_nonnulls(build_execution_id,generation,published_at) = 0)
    )
);
CREATE UNIQUE INDEX one_working_revision ON ontology.revisions(tenant_id, ontology_id)
    WHERE status IN ('draft','in_review');

CREATE TABLE ontology.revision_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    ontology_id VARCHAR(64) NOT NULL,
    revision BIGINT NOT NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    action VARCHAR(16) NOT NULL CHECK (action IN ('create','save','submit','return','publish','withdraw')),
    from_state VARCHAR(16) NOT NULL,
    to_state VARCHAR(16) NOT NULL,
    digest VARCHAR(64) NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
    actor_principal_id BIGINT NOT NULL CHECK (actor_principal_id > 0),
    actor_membership_id BIGINT NOT NULL CHECK (actor_membership_id > 0),
    authorization_version BIGINT NOT NULL CHECK (authorization_version > 0),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (tenant_id, ontology_id, revision, version),
    FOREIGN KEY (tenant_id, ontology_id, revision) REFERENCES ontology.revisions(tenant_id, ontology_id, revision)
);

CREATE FUNCTION ontology.guard_revision() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'ontology revision deletion is not supported' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'draft' OR NEW.version <> 1 THEN
            RAISE EXCEPTION 'revision must start as draft version 1' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF (NEW.tenant_id, NEW.ontology_id, NEW.revision, NEW.created_at)
        IS DISTINCT FROM (OLD.tenant_id, OLD.ontology_id, OLD.revision, OLD.created_at)
        OR NEW.version <> OLD.version + 1 THEN
        RAISE EXCEPTION 'revision identity or version violation' USING ERRCODE = '23514';
    END IF;
    IF NOT ((OLD.status = 'draft' AND NEW.status IN ('draft','in_review'))
        OR (OLD.status = 'in_review' AND NEW.status IN ('draft','published'))
        OR (OLD.status = 'published' AND NEW.status = 'withdrawn')) THEN
        RAISE EXCEPTION 'invalid revision transition' USING ERRCODE = '23514';
    END IF;
    IF (OLD.status <> 'draft' OR NEW.status <> 'draft') AND
        (NEW.payload, NEW.digest) IS DISTINCT FROM (OLD.payload, OLD.digest) THEN
        RAISE EXCEPTION 'reviewed or published content is immutable' USING ERRCODE = '23514';
    END IF;
    IF OLD.status = 'published' AND (NEW.build_execution_id, NEW.generation, NEW.published_at)
        IS DISTINCT FROM (OLD.build_execution_id, OLD.generation, OLD.published_at) THEN
        RAISE EXCEPTION 'publication identity is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_revision BEFORE INSERT OR UPDATE OR DELETE ON ontology.revisions
    FOR EACH ROW EXECUTE FUNCTION ontology.guard_revision();

CREATE FUNCTION ontology.guard_revision_event() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'revision events are append only' USING ERRCODE = '23514';
END $$;
CREATE TRIGGER guard_revision_event BEFORE UPDATE OR DELETE ON ontology.revision_events
    FOR EACH ROW EXECUTE FUNCTION ontology.guard_revision_event();
