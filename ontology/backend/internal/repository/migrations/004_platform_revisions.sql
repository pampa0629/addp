-- Platform identity is separate from Tenant ontology identity. Recording does
-- not publish, build, activate or authorize a release.
CREATE TABLE ontology.platform_capabilities (
    capability TEXT PRIMARY KEY CHECK (
        octet_length(capability) <= 128 AND
        capability ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'
    ),
    last_revision BIGINT NOT NULL DEFAULT 0 CHECK (last_revision >= 0)
);

CREATE TABLE ontology.platform_revisions (
    capability TEXT NOT NULL REFERENCES ontology.platform_capabilities(capability),
    revision BIGINT NOT NULL CHECK (revision > 0),
    payload TEXT NOT NULL CHECK (octet_length(payload) <= 65536),
    digest TEXT NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (capability, revision),
    UNIQUE (capability, revision, digest),
    CONSTRAINT platform_payload_digest CHECK (
        encode(sha256(convert_to(payload, 'UTF8')), 'hex') = digest
    ),
    CONSTRAINT platform_payload_identity CHECK (COALESCE(
        jsonb_typeof(payload::jsonb) = 'object' AND
        payload::jsonb->>'contract' = 'addp.platform-definition/v1' AND
        payload::jsonb->>'compiler' = 'addp.platform-compiler/v1' AND
        payload::jsonb#>>'{definition,schema_version}' = 'addp.platform-capability-context/v1' AND
        payload::jsonb#>>'{definition,knowledge_kind}' = 'platform_definition' AND
        payload::jsonb#>>'{definition,capability}' = capability AND
        jsonb_typeof(payload::jsonb#>'{definition,revision}') = 'number' AND
        payload::jsonb#>>'{definition,revision}' = revision::text AND
        payload::jsonb#>>'{definition,digest}' = '' AND
        payload::jsonb#>>'{definition,availability}' = 'not_observed' AND
        NOT (payload::jsonb->'definition' ? 'tenant_id'), FALSE
    ))
);

CREATE TABLE ontology.platform_revision_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    capability TEXT NOT NULL,
    revision BIGINT NOT NULL,
    action TEXT NOT NULL CHECK (action = 'record'),
    digest TEXT NOT NULL,
    actor_principal_id BIGINT NOT NULL CHECK (actor_principal_id > 0),
    actor_principal_type TEXT NOT NULL CHECK (actor_principal_type IN ('user', 'service_principal')),
    authorization_version BIGINT NOT NULL CHECK (authorization_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (capability, revision, action),
    FOREIGN KEY (capability, revision, digest)
        REFERENCES ontology.platform_revisions(capability, revision, digest)
);

CREATE FUNCTION ontology.guard_platform_record_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'platform revision and audit records are immutable' USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER platform_revision_immutable
BEFORE UPDATE OR DELETE ON ontology.platform_revisions
FOR EACH ROW EXECUTE FUNCTION ontology.guard_platform_record_immutable();

CREATE TRIGGER platform_revision_event_immutable
BEFORE UPDATE OR DELETE ON ontology.platform_revision_events
FOR EACH ROW EXECUTE FUNCTION ontology.guard_platform_record_immutable();
