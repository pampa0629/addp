BEGIN;

-- Old native-target records are disposable test data (explicitly approved).
-- Clear only source-authorization facts; keep IAM, delegations and generic audits.
LOCK TABLE system.engine_access_grant_revocations, system.engine_access_grants,
 system.engine_access_fulfillment_outcomes, system.engine_access_approval_requirements,
 system.engine_access_deny_releases, system.engine_access_denies IN ACCESS EXCLUSIVE MODE;
ALTER TABLE system.engine_access_grant_revocations DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_grants DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_fulfillment_outcomes DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_approval_requirements DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_deny_releases DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_denies DISABLE TRIGGER USER;
DELETE FROM system.engine_access_grant_revocations;
DELETE FROM system.engine_access_grants;
DELETE FROM system.engine_access_fulfillment_outcomes;
DELETE FROM system.engine_access_approval_requirements;
DELETE FROM system.engine_access_deny_releases;
DELETE FROM system.engine_access_denies;
ALTER TABLE system.engine_access_grant_revocations ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_grants ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_fulfillment_outcomes ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_approval_requirements ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_deny_releases ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_denies ENABLE TRIGGER USER;

-- The native coordinate is an immutable single-table boundary/audit snapshot,
-- never a second ACL key. This slice explicitly rejects other layouts.
CREATE FUNCTION system.valid_source_single_identity(path jsonb, engine bigint, fingerprint text)
RETURNS boolean LANGUAGE sql IMMUTABLE STRICT SET search_path = pg_catalog AS $$
 SELECT (jsonb_typeof(path) = 'object' AND path->>'version' = 'catalog.path/v1'
 AND path->>'engine_id' = engine::text AND engine > 0
 AND jsonb_typeof(path->'segments') = 'array' AND jsonb_array_length(path->'segments') = 3
 AND path->'segments'->0->>'term' = 'server' AND path->'segments'->0->>'kind' = 'server'
 AND path->'segments'->1->>'term' = 'schema' AND path->'segments'->1->>'kind' = 'schema'
 AND path->'segments'->2->>'term' = 'table' AND path->'segments'->2->>'kind' = 'table'
 AND length(path->'segments'->1->>'name') > 0 AND length(path->'segments'->2->>'name') > 0
 AND fingerprint ~ '^[0-9a-f]{64}$'
 AND fingerprint = encode(sha256(convert_to(engine::text || ':' ||
   (path->'segments'->1->>'name') || '.' || (path->'segments'->2->>'name'), 'UTF8')), 'hex')) IS TRUE
$$;

ALTER TABLE system.engine_access_grants ADD COLUMN item_fingerprint text NOT NULL,
 ADD COLUMN boundary_hash text NOT NULL,
 ADD CONSTRAINT source_grant_dataitem_identity CHECK (system.valid_source_single_identity(catalog_path, engine_id, item_fingerprint)),
 ADD CONSTRAINT source_grant_boundary_hash CHECK (boundary_hash ~ '^[0-9a-f]{64}$');
ALTER TABLE system.engine_access_approval_requirements ADD COLUMN item_fingerprint text NOT NULL,
 ADD CONSTRAINT source_approval_dataitem_identity CHECK (system.valid_source_single_identity(catalog_path, engine_id, item_fingerprint));
ALTER TABLE system.engine_access_denies ADD COLUMN item_fingerprint text NOT NULL,
 ADD CONSTRAINT source_deny_dataitem_identity CHECK (system.valid_source_single_identity(catalog_path, engine_id, item_fingerprint));
ALTER TABLE system.engine_access_fulfillment_outcomes
 ADD CONSTRAINT source_fulfillment_dataitem_binding CHECK ((system.valid_source_single_identity(catalog_path, engine_id, binding->>'item_fingerprint')
 AND binding->>'boundary_hash' ~ '^[0-9a-f]{64}$') IS TRUE);

CREATE INDEX idx_source_grant_dataitem ON system.engine_access_grants(tenant_id, engine_id, item_fingerprint, boundary_hash);
CREATE INDEX idx_source_deny_dataitem ON system.engine_access_denies(tenant_id, engine_id, item_fingerprint);

CREATE OR REPLACE FUNCTION system.guard_engine_access_approval_requirement() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
 IF TG_OP IN ('DELETE', 'TRUNCATE') THEN
  RAISE EXCEPTION 'engine access approval requirement cannot be removed' USING ERRCODE = '23514';
 END IF;
 IF TG_OP = 'INSERT' THEN
  IF NEW.version <> 1 THEN RAISE EXCEPTION 'initial approval requirement version must be one' USING ERRCODE = '23514'; END IF;
  NEW.target_digest := sha256(convert_to(NEW.item_fingerprint, 'UTF8'));
 ELSE
  IF (NEW.id, NEW.tenant_id, NEW.engine_id, NEW.catalog_path, NEW.item_fingerprint, NEW.target_digest)
   IS DISTINCT FROM (OLD.id, OLD.tenant_id, OLD.engine_id, OLD.catalog_path, OLD.item_fingerprint, OLD.target_digest)
   OR NEW.version <> OLD.version + 1 THEN
   RAISE EXCEPTION 'approval requirement identity is immutable and version must advance once' USING ERRCODE = '23514';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION system.guard_engine_access_grant_insert() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
DECLARE
    receipt system.engine_access_fulfillment_outcomes%ROWTYPE;
BEGIN
    NEW.granted_at := clock_timestamp();
    IF NEW.approval_mode = 'catalog' THEN
        SELECT * INTO receipt FROM system.engine_access_fulfillment_outcomes
        WHERE request_id = NEW.request_id AND outcome = 'accepted';
        IF receipt.deadline IS NULL OR NEW.granted_at < receipt.recorded_at OR NEW.granted_at >= receipt.deadline THEN
            RAISE EXCEPTION 'engine access grant requires an unexpired accepted fulfillment'
                USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_acceptance_window';
        END IF;
        -- Rule facts come from the trusted receipt, never caller overrides.
        NEW.catalog_request_id := receipt.request_id;
        NEW.item_fingerprint := receipt.binding->>'item_fingerprint';
        NEW.boundary_hash := receipt.binding->>'boundary_hash';
        NEW.tenant_id := receipt.tenant_id; NEW.engine_id := receipt.engine_id; NEW.catalog_path := receipt.catalog_path;
        NEW.recipient_type := receipt.binding->>'recipient_type'; NEW.recipient_id := (receipt.binding->>'recipient_id')::bigint;
        NEW.action := receipt.binding->>'action'; NEW.expiry_mode := receipt.expiry_mode; NEW.expires_at := receipt.grant_expires_at;
        NEW.requirement_version := (receipt.binding->>'requirement_version')::bigint;
        NEW.operator_principal_id := (receipt.binding->'operator'->>'principal_id')::bigint;
        NEW.operator_membership_id := (receipt.binding->'operator'->>'tenant_membership_id')::bigint;
        NEW.operator_authorization_version := (receipt.binding->'operator'->>'authorization_version')::bigint;
        NEW.reason := NULL;
    ELSIF NEW.approval_mode = 'independent' THEN
        -- No synthetic Catalog receipt. The command service must separately
        -- hold current IAM/delegation and the same precise target boundary.
        IF NOT EXISTS (SELECT 1 FROM system.engine_access_approval_requirements r
            WHERE r.tenant_id = NEW.tenant_id AND r.engine_id = NEW.engine_id
                AND r.item_fingerprint = NEW.item_fingerprint AND r.mode = 'independent' AND r.version = NEW.requirement_version) THEN
            RAISE EXCEPTION 'independent approval requirement does not match'
                USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_approval_requirement';
        END IF;
        IF NOT EXISTS (SELECT 1 FROM system.tenant_memberships m JOIN system.principals p ON p.id = m.principal_id
            JOIN system.tenants t ON t.id = m.tenant_id
            JOIN system.engines e ON e.tenant_id = t.id AND e.id = NEW.engine_id
            WHERE m.id = NEW.operator_membership_id AND p.id = NEW.operator_principal_id AND m.tenant_id = NEW.tenant_id
                AND p.principal_type = 'user' AND p.status = 'active' AND p.authorization_version = NEW.operator_authorization_version
                AND m.status = 'active' AND m.joined_at <= NEW.granted_at AND (m.expires_at IS NULL OR m.expires_at > NEW.granted_at)
                AND t.status = 'active' AND e.lifecycle_state = 'active') THEN
            RAISE EXCEPTION 'independent grant actor must be current in the source tenant'
                USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_actor';
        END IF;
        IF NOT ((NEW.recipient_type = 'user' AND EXISTS (
            SELECT 1 FROM system.tenant_memberships m JOIN system.principals p ON p.id = m.principal_id
            WHERE m.tenant_id = NEW.tenant_id AND p.id = NEW.recipient_id AND p.principal_type = 'user'
                AND p.status = 'active' AND m.status = 'active' AND m.joined_at <= NEW.granted_at
                AND (m.expires_at IS NULL OR m.expires_at > NEW.granted_at)))
            OR (NEW.recipient_type = 'department' AND EXISTS (SELECT 1 FROM system.departments d
                WHERE d.tenant_id = NEW.tenant_id AND d.id = NEW.recipient_id AND d.status = 'active'))
            OR (NEW.recipient_type = 'project_group' AND EXISTS (SELECT 1 FROM system.project_groups g
                WHERE g.tenant_id = NEW.tenant_id AND g.id = NEW.recipient_id AND g.status = 'active'))) THEN
            RAISE EXCEPTION 'independent grant recipient must be current in the source tenant'
                USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_recipient';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION system.guard_source_grant_relation() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(
        'source_grant_relation|' || jsonb_build_array(NEW.tenant_id, NEW.engine_id,
            NEW.item_fingerprint, NEW.recipient_type, NEW.recipient_id, NEW.action)::text, 0));
    IF EXISTS (SELECT 1 FROM system.engine_access_grants g
        WHERE g.request_id <> NEW.request_id AND g.tenant_id = NEW.tenant_id AND g.engine_id = NEW.engine_id
            AND g.item_fingerprint = NEW.item_fingerprint AND g.recipient_type = NEW.recipient_type
            AND g.recipient_id = NEW.recipient_id AND g.action = NEW.action
            AND g.granted_at <= clock_timestamp()
            AND (g.expires_at IS NULL OR g.expires_at > clock_timestamp())
            AND NOT EXISTS (SELECT 1 FROM system.engine_access_grant_revocations r WHERE r.request_id = g.request_id)) THEN
        RAISE EXCEPTION 'source authorization relation already exists; explicit withdrawal is required before changing expiry'
            USING ERRCODE = '23505', CONSTRAINT = 'engine_access_grant_relation_exists';
    END IF;
    RETURN NEW;
END;
$$;

COMMIT;
