BEGIN;
CREATE TABLE system.host_nodes (
    node_id uuid PRIMARY KEY CHECK (node_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    display_name varchar(255) NOT NULL CHECK (length(btrim(display_name)) > 0),
    node_kind varchar(20) NOT NULL CHECK (node_kind IN ('physical','virtual')),
    addresses jsonb NOT NULL CHECK (jsonb_typeof(addresses) = 'array' AND jsonb_array_length(addresses) <= 32),
    enabled boolean NOT NULL,
    allowed_module_bindings jsonb NOT NULL CHECK (jsonb_typeof(allowed_module_bindings) = 'array' AND jsonb_array_length(allowed_module_bindings) <= 64),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
ALTER TABLE system.module_runtime_instances
    ADD COLUMN declared_node_id varchar(36) NOT NULL DEFAULT '' CHECK (declared_node_id = '' OR
        (declared_node_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
        AND declared_node_id <> '00000000-0000-0000-0000-000000000000')),
    ADD COLUMN registration_client_id varchar(100) NOT NULL DEFAULT '';
-- Unknown/disabled declarations are retained without a foreign key to avoid blocking leases.
CREATE FUNCTION system.guard_module_node_declaration() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.declared_node_id IS DISTINCT FROM OLD.declared_node_id
       OR NEW.registration_client_id IS DISTINCT FROM OLD.registration_client_id THEN
        RAISE EXCEPTION 'module node declaration is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER module_node_declaration_immutable BEFORE UPDATE ON system.module_runtime_instances
    FOR EACH ROW EXECUTE FUNCTION system.guard_module_node_declaration();

INSERT INTO system.permissions (permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status)
VALUES
    ('platform.host_node.create','system','create','medium',false,ARRAY['platform']::text[],false,'permissions.platform.host_node.create.name','permissions.platform.host_node.create.description','active'),
    ('platform.host_node.read','system','read','low',false,ARRAY['platform']::text[],false,'permissions.platform.host_node.read.name','permissions.platform.host_node.read.description','active'),
    ('platform.host_node.update','system','update','medium',false,ARRAY['platform']::text[],false,'permissions.platform.host_node.update.name','permissions.platform.host_node.update.description','active');
INSERT INTO system.role_permissions (role_id,permission_id,source_type)
SELECT role.id,permission.id,'product' FROM system.roles role JOIN system.permissions permission
ON permission.permission_key IN ('platform.host_node.create','platform.host_node.read','platform.host_node.update')
WHERE role.role_key = 'platform.system_administrator' AND role.tenant_id IS NULL AND role.status = 'active'
ON CONFLICT DO NOTHING;
CREATE TEMP TABLE host_node_affected_principals ON COMMIT DROP AS
SELECT DISTINCT assignment.principal_id FROM system.role_assignments assignment
JOIN system.roles role ON role.id = assignment.role_id
WHERE assignment.status = 'active' AND role.role_key = 'platform.system_administrator' AND role.tenant_id IS NULL;
UPDATE system.principals principal SET authorization_version = principal.authorization_version + 1, updated_at = now()
FROM host_node_affected_principals affected WHERE principal.id = affected.principal_id;
UPDATE system.refresh_token_families family SET revoked_at = now(), revoked_reason = 'authorization_catalog_changed', updated_at = now()
FROM host_node_affected_principals affected WHERE family.principal_id = affected.principal_id AND family.revoked_at IS NULL;
COMMIT;
