BEGIN;

-- Internal coordination only; no content grant or new role/permission.
CREATE TABLE system.engine_access_fulfillment_outcomes (
    request_id uuid PRIMARY KEY CHECK (request_id <> '00000000-0000-0000-0000-000000000000'),
    tenant_id bigint NOT NULL REFERENCES system.tenants(id),
    engine_id bigint NOT NULL,
    caller_principal_id bigint NOT NULL REFERENCES system.service_principals(id),
    catalog_path jsonb NOT NULL CHECK (jsonb_typeof(catalog_path) = 'object'),
    binding jsonb NOT NULL CHECK (jsonb_typeof(binding) = 'object'),
    grant_expires_at timestamptz NOT NULL CHECK (isfinite(grant_expires_at)),
    outcome text NOT NULL CHECK (outcome IN ('accepted', 'closed')),
    recorded_at timestamptz NOT NULL CHECK (isfinite(recorded_at)),
    deadline timestamptz,
    FOREIGN KEY (tenant_id, engine_id) REFERENCES system.engines(tenant_id, id),
    CHECK ((outcome = 'closed' AND deadline IS NULL)
        OR (outcome = 'accepted' AND deadline IS NOT NULL AND deadline > recorded_at
            AND deadline = LEAST(recorded_at + interval '5 minutes', grant_expires_at)))
);
CREATE INDEX idx_engine_access_fulfillment_target ON system.engine_access_fulfillment_outcomes (tenant_id, engine_id);
CREATE INDEX idx_engine_access_fulfillment_caller ON system.engine_access_fulfillment_outcomes (caller_principal_id);

CREATE FUNCTION system.preserve_engine_access_fulfillment_outcome() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'engine access fulfillment outcome is immutable' USING ERRCODE = '23514';
END;
$$;
CREATE TRIGGER trg_engine_access_fulfillment_immutable BEFORE UPDATE OR DELETE
ON system.engine_access_fulfillment_outcomes FOR EACH ROW
EXECUTE FUNCTION system.preserve_engine_access_fulfillment_outcome();
CREATE TRIGGER trg_engine_access_fulfillment_no_truncate BEFORE TRUNCATE
ON system.engine_access_fulfillment_outcomes FOR EACH STATEMENT
EXECUTE FUNCTION system.preserve_engine_access_fulfillment_outcome();

COMMIT;
