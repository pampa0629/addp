BEGIN;
CREATE TABLE system.engine_deployments (
    deployment_id uuid PRIMARY KEY CHECK (deployment_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    display_name varchar(255) NOT NULL CHECK (length(btrim(display_name)) > 0),
    engine_type varchar(50) NOT NULL CHECK (engine_type = 'postgresql'),
    enabled boolean NOT NULL,
    inventory jsonb NOT NULL CHECK (COALESCE(jsonb_typeof(inventory) = 'object'
        AND inventory->>'schema' = 'addp.business-deployment-inventory/v1'
        AND inventory->'source'->>'type' = 'docker_compose'
        AND inventory->'source'->>'runtime' IN ('linux_docker','desktop_linux_vm')
        AND inventory->'source'->>'project' = 'business'
        AND inventory->'source'->>'docker_engine_id' ~ '^[A-Za-z0-9][A-Za-z0-9:_.-]{0,127}$'
        AND jsonb_typeof(inventory->'services') = 'array' AND jsonb_array_length(inventory->'services') = 1
        AND inventory->'services'->0->>'service' = 'postgres'
        AND inventory->'services'->0->>'engine_type' = 'postgresql', false)),
    declared_node_id uuid NOT NULL REFERENCES system.host_nodes(node_id),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at)
);
CREATE UNIQUE INDEX engine_deployment_source_unique ON system.engine_deployments
    ((inventory->'source'->>'docker_engine_id'),(inventory->'source'->>'project'),(inventory->'services'->0->>'service'));
CREATE INDEX engine_deployment_host_index ON system.engine_deployments(declared_node_id);
CREATE TABLE system.engine_deployment_declarations (
    deployment_id uuid NOT NULL REFERENCES system.engine_deployments(deployment_id),
    version bigint NOT NULL CHECK (version > 0),
    node_id uuid NOT NULL REFERENCES system.host_nodes(node_id),
    inventory jsonb NOT NULL CHECK (jsonb_typeof(inventory) = 'object'),
    enabled boolean NOT NULL,
    valid_from timestamptz NOT NULL,
    valid_to timestamptz CHECK (valid_to IS NULL OR valid_to > valid_from),
    PRIMARY KEY(deployment_id,version)
);
CREATE UNIQUE INDEX engine_deployment_current_declaration ON system.engine_deployment_declarations(deployment_id) WHERE valid_to IS NULL;
CREATE FUNCTION system.guard_engine_deployment_declaration() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'deployment declaration history is immutable' USING ERRCODE='23514'; END IF;
    IF OLD.valid_to IS NOT NULL OR NEW.valid_to IS NULL
       OR ROW(NEW.deployment_id,NEW.version,NEW.node_id,NEW.inventory,NEW.enabled,NEW.valid_from)
          IS DISTINCT FROM ROW(OLD.deployment_id,OLD.version,OLD.node_id,OLD.inventory,OLD.enabled,OLD.valid_from) THEN
        RAISE EXCEPTION 'deployment declaration history is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER engine_deployment_declaration_immutable BEFORE UPDATE OR DELETE ON system.engine_deployment_declarations
    FOR EACH ROW EXECUTE FUNCTION system.guard_engine_deployment_declaration();

INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
SELECT 'platform.engine_deployment.'||action,'system',action,risk,false,ARRAY['platform']::text[],false,
    'permissions.platform.engine_deployment.'||action||'.name','permissions.platform.engine_deployment.'||action||'.description','active'
FROM (VALUES ('read','low'),('create','medium'),('update','medium')) seed(action,risk);
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p
    ON p.permission_key IN ('platform.engine_deployment.read','platform.engine_deployment.create','platform.engine_deployment.update')
WHERE r.tenant_id IS NULL AND r.role_key='platform.system_administrator';
-- Product role-permission triggers advance authorization_version. Existing refresh families are revoked.
UPDATE system.refresh_token_families f
SET revoked_at=statement_timestamp(),revoked_reason='authorization_catalog_changed',updated_at=statement_timestamp()
WHERE f.revoked_at IS NULL AND EXISTS (
    SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
    WHERE a.principal_id=f.principal_id AND a.status='active' AND a.scope_type='platform'
    AND r.tenant_id IS NULL AND r.role_key='platform.system_administrator');
COMMIT;
