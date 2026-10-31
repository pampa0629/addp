BEGIN;
-- User-approved development reset. Remove old data rules; preserve IAM,
-- delegation, collection qualifications, source data and generic audit.
LOCK TABLE system.engine_access_grant_revocations, system.engine_access_grants,
 system.engine_access_initial_grant_outcomes, system.engine_access_fulfillment_outcomes,
 system.engine_access_deny_releases, system.engine_access_denies IN ACCESS EXCLUSIVE MODE;
ALTER TABLE system.engine_access_grant_revocations DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_grants DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_initial_grant_outcomes DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_fulfillment_outcomes DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_deny_releases DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_denies DISABLE TRIGGER USER;

-- Both directions of the production outcome / Grant relation are deferred.
-- Keep only the terminal refusal keys: forgetting them would make a replay of
-- an already issued production look like a first-ever authorization request.
SET CONSTRAINTS ALL DEFERRED;
DELETE FROM system.engine_access_grant_revocations;
DELETE FROM system.engine_access_grants;
UPDATE system.engine_access_initial_grant_outcomes
 SET outcome = 'denied', grant_request_id = NULL, denial_code = 'target_history_exists'
 WHERE outcome = 'issued';
DELETE FROM system.engine_access_fulfillment_outcomes;
DELETE FROM system.engine_access_deny_releases;
DELETE FROM system.engine_access_denies;
SET CONSTRAINTS ALL IMMEDIATE;

ALTER TABLE system.engine_access_grant_revocations ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_grants ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_initial_grant_outcomes ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_fulfillment_outcomes ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_deny_releases ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_denies ENABLE TRIGGER USER;


-- Remove the retired per-DataItem permission model.
ALTER TABLE system.engine_access_grants
 DROP CONSTRAINT source_grant_direct_permission_provenance,
 DROP COLUMN direct_permission_source, DROP COLUMN direct_permission_version,
 ADD CONSTRAINT source_grant_operator_provenance CHECK (operator_authorization_version > 0);
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
        NEW.operator_principal_id := (receipt.binding->'operator'->>'principal_id')::bigint;
        NEW.operator_membership_id := (receipt.binding->'operator'->>'tenant_membership_id')::bigint;
        NEW.operator_authorization_version := (receipt.binding->'operator'->>'authorization_version')::bigint;
        NEW.reason := NULL;
    ELSIF NEW.approval_mode = 'independent' THEN
        -- Current IAM, engine scope and boundary are enforced by the canonical command.
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
DROP TABLE system.engine_access_direct_permissions;
DROP FUNCTION system.guard_engine_access_direct_permission();
CREATE TEMP TABLE retired_direct_permission_principals ON COMMIT DROP AS
 SELECT DISTINCT a.principal_id FROM system.role_assignments a
 JOIN system.role_permissions rp ON rp.role_id=a.role_id
 JOIN system.permissions p ON p.id=rp.permission_id
 WHERE a.status='active' AND p.permission_key IN ('system.engine_access_direct_permission.read','system.engine_access_direct_permission.update');
DELETE FROM system.role_permissions rp USING system.permissions p WHERE rp.permission_id=p.id
 AND p.permission_key IN ('system.engine_access_direct_permission.read','system.engine_access_direct_permission.update');
UPDATE system.permissions SET status='disabled',updated_at=now() WHERE permission_key IN ('system.engine_access_direct_permission.read','system.engine_access_direct_permission.update');
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
 FROM retired_direct_permission_principals a WHERE f.principal_id=a.principal_id AND f.revoked_at IS NULL;
COMMIT;
