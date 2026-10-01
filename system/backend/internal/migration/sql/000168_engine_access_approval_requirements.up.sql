BEGIN;

-- Internal authority fact only. No source access, permission or role is granted.
CREATE TABLE system.engine_access_approval_requirements (
    id uuid PRIMARY KEY CHECK (id <> '00000000-0000-0000-0000-000000000000'),
    tenant_id bigint NOT NULL REFERENCES system.tenants(id),
    engine_id bigint NOT NULL,
    catalog_path jsonb NOT NULL CHECK (jsonb_typeof(catalog_path) = 'object' AND catalog_path <> '{}'::jsonb),
    target_digest bytea NOT NULL CHECK (octet_length(target_digest) = 32),
    mode text NOT NULL CHECK (mode IN ('catalog', 'independent')),
    version bigint NOT NULL CHECK (version > 0),
    updated_at timestamptz NOT NULL CHECK (isfinite(updated_at)),
    FOREIGN KEY (tenant_id, engine_id) REFERENCES system.engines(tenant_id, id),
    UNIQUE (tenant_id, engine_id, target_digest)
);

-- Bound index size for long structured paths. A digest is an index hint, not
-- identity proof: repository reads also require complete JSONB equality.
CREATE FUNCTION system.guard_engine_access_approval_requirement() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    IF TG_OP IN ('DELETE', 'TRUNCATE') THEN
        RAISE EXCEPTION 'engine access approval requirement cannot be removed' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.version <> 1 THEN
            RAISE EXCEPTION 'initial approval requirement version must be one' USING ERRCODE = '23514';
        END IF;
        NEW.target_digest := sha256(convert_to(NEW.catalog_path::text, 'UTF8'));
    ELSE
        IF (NEW.id, NEW.tenant_id, NEW.engine_id, NEW.catalog_path, NEW.target_digest)
            IS DISTINCT FROM (OLD.id, OLD.tenant_id, OLD.engine_id, OLD.catalog_path, OLD.target_digest)
            OR NEW.version <> OLD.version + 1 THEN
            RAISE EXCEPTION 'approval requirement identity is immutable and version must advance once' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_engine_access_approval_requirement_guard BEFORE INSERT OR UPDATE OR DELETE
ON system.engine_access_approval_requirements FOR EACH ROW
EXECUTE FUNCTION system.guard_engine_access_approval_requirement();
CREATE TRIGGER trg_engine_access_approval_requirement_no_truncate BEFORE TRUNCATE
ON system.engine_access_approval_requirements FOR EACH STATEMENT
EXECUTE FUNCTION system.guard_engine_access_approval_requirement();

COMMIT;
