BEGIN;

-- The Grant owns the source rule. A Catalog receipt proves one approval
-- provenance, not the parameters consulted by every subsequent read/revoke.
ALTER TABLE system.engine_access_grants
    DROP CONSTRAINT engine_access_grants_request_id_fkey,
    ADD COLUMN approval_mode text,
    ADD COLUMN catalog_request_id uuid REFERENCES system.engine_access_fulfillment_outcomes(request_id),
    ADD COLUMN tenant_id bigint REFERENCES system.tenants(id),
    ADD COLUMN engine_id bigint,
    ADD COLUMN catalog_path jsonb,
    ADD COLUMN recipient_type text,
    ADD COLUMN recipient_id bigint,
    ADD COLUMN action text,
    ADD COLUMN expiry_mode text,
    ADD COLUMN expires_at timestamptz,
    ADD COLUMN requirement_version bigint,
    ADD COLUMN operator_principal_id bigint REFERENCES system.principals(id),
    ADD COLUMN operator_membership_id bigint REFERENCES system.tenant_memberships(id),
    ADD COLUMN operator_authorization_version bigint,
    ADD COLUMN reason text;

-- Forward-only structural projection. Preserve original IDs, issuance times,
-- receipt history and revocations; never generate another Grant or audit.
ALTER TABLE system.engine_access_grants DISABLE TRIGGER trg_engine_access_grant_immutable;
UPDATE system.engine_access_grants g SET
    approval_mode = 'catalog', catalog_request_id = g.request_id,
    tenant_id = o.tenant_id, engine_id = o.engine_id, catalog_path = o.catalog_path,
    recipient_type = o.binding->>'recipient_type', recipient_id = (o.binding->>'recipient_id')::bigint,
    action = o.binding->>'action', expiry_mode = o.expiry_mode, expires_at = o.grant_expires_at,
    requirement_version = (o.binding->>'requirement_version')::bigint,
    operator_principal_id = (o.binding->'operator'->>'principal_id')::bigint,
    operator_membership_id = (o.binding->'operator'->>'tenant_membership_id')::bigint,
    operator_authorization_version = (o.binding->'operator'->>'authorization_version')::bigint
FROM system.engine_access_fulfillment_outcomes o WHERE o.request_id = g.request_id AND o.outcome = 'accepted';
ALTER TABLE system.engine_access_grants ENABLE TRIGGER trg_engine_access_grant_immutable;

ALTER TABLE system.engine_access_grants
    ALTER COLUMN approval_mode SET NOT NULL,
    ALTER COLUMN tenant_id SET NOT NULL, ALTER COLUMN engine_id SET NOT NULL,
    ALTER COLUMN catalog_path SET NOT NULL, ALTER COLUMN recipient_type SET NOT NULL,
    ALTER COLUMN recipient_id SET NOT NULL, ALTER COLUMN action SET NOT NULL,
    ALTER COLUMN expiry_mode SET NOT NULL, ALTER COLUMN requirement_version SET NOT NULL,
    ALTER COLUMN operator_principal_id SET NOT NULL, ALTER COLUMN operator_membership_id SET NOT NULL,
    ALTER COLUMN operator_authorization_version SET NOT NULL,
    ADD CONSTRAINT engine_access_grant_command CHECK (request_id <> '00000000-0000-0000-0000-000000000000'),
    ADD CONSTRAINT engine_access_grant_engine FOREIGN KEY (tenant_id, engine_id) REFERENCES system.engines(tenant_id, id),
    ADD CONSTRAINT engine_access_grant_origin CHECK (
        (approval_mode = 'catalog' AND catalog_request_id IS NOT NULL AND catalog_request_id = request_id AND reason IS NULL)
        OR (approval_mode = 'independent' AND catalog_request_id IS NULL
            AND reason IS NOT NULL AND reason = btrim(reason) AND length(reason) BETWEEN 1 AND 2000)),
    ADD CONSTRAINT engine_access_grant_target CHECK ((jsonb_typeof(catalog_path) = 'object'
        AND catalog_path ? 'engine_id' AND (catalog_path->>'engine_id')::bigint = engine_id) IS TRUE),
    ADD CONSTRAINT engine_access_grant_subject CHECK (recipient_type IN ('user', 'department', 'project_group') AND recipient_id > 0),
    ADD CONSTRAINT engine_access_grant_action CHECK (action = 'read'),
    ADD CONSTRAINT engine_access_grant_provenance CHECK (requirement_version > 0 AND operator_authorization_version > 0),
    ADD CONSTRAINT engine_access_grant_expiry CHECK (
        (expiry_mode = 'until_revoked' AND expires_at IS NULL)
        OR (expiry_mode = 'at_time' AND expires_at IS NOT NULL AND isfinite(expires_at) AND expires_at > granted_at));

CREATE INDEX idx_engine_access_grants_target ON system.engine_access_grants(tenant_id, engine_id);
CREATE INDEX idx_engine_access_grants_catalog_request ON system.engine_access_grants(catalog_request_id);
CREATE INDEX idx_engine_access_grants_operator ON system.engine_access_grants(operator_principal_id);
CREATE INDEX idx_engine_access_grants_membership ON system.engine_access_grants(operator_membership_id);

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
                AND r.catalog_path = NEW.catalog_path AND r.mode = 'independent' AND r.version = NEW.requirement_version) THEN
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

-- Withdrawal consumes the same Grant facts for every approval origin.
CREATE OR REPLACE FUNCTION system.guard_engine_access_grant_revocation_insert() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    NEW.revoked_at := clock_timestamp();
    IF NOT EXISTS (SELECT 1 FROM system.engine_access_grants g
        JOIN system.tenant_memberships m ON m.tenant_id = g.tenant_id JOIN system.principals p ON p.id = m.principal_id
        WHERE g.request_id = NEW.request_id AND g.granted_at <= NEW.revoked_at
            AND m.id = NEW.revoked_by_membership_id AND p.id = NEW.revoked_by_principal_id AND p.principal_type = 'user') THEN
        RAISE EXCEPTION 'grant revoker must reference a user membership in the original tenant'
            USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_revoker_binding';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM system.engine_access_grants g WHERE g.request_id = NEW.request_id
        AND ((g.expiry_mode = 'until_revoked' AND g.expires_at IS NULL) OR (g.expiry_mode = 'at_time' AND g.expires_at > NEW.revoked_at))) THEN
        RAISE EXCEPTION 'grant has expired; revocation is unnecessary'
            USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_revocation_expiry';
    END IF;
    RETURN NEW;
END;
$$;

COMMIT;
