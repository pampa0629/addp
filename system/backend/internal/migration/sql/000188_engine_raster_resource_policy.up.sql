BEGIN;

CREATE TABLE system.engine_raster_policies (
    engine_id bigint PRIMARY KEY REFERENCES system.engines(id),
    version bigint NOT NULL CHECK (version > 0),
    running integer NOT NULL CHECK (running BETWEEN 1 AND 64),
    waiting integer NOT NULL CHECK (waiting BETWEEN 0 AND 1024),
    cache_mib integer NOT NULL CHECK (cache_mib BETWEEN 16 AND 65536),
    default_tenant_running integer NOT NULL CHECK (default_tenant_running BETWEEN 1 AND running),
    default_tenant_waiting integer NOT NULL CHECK (default_tenant_waiting BETWEEN 0 AND waiting)
);
CREATE TABLE system.engine_raster_quotas (
    engine_id bigint NOT NULL REFERENCES system.engine_raster_policies(engine_id),
    tenant_id bigint NOT NULL REFERENCES system.tenants(id),
    version bigint NOT NULL CHECK (version > 0),
    running integer CHECK (running BETWEEN 1 AND 64),
    waiting integer CHECK (waiting BETWEEN 0 AND 1024),
    PRIMARY KEY (engine_id, tenant_id)
);
INSERT INTO system.engine_raster_policies
SELECT id, 1, 2, 2, 256, 2, 2 FROM system.engines
WHERE engine_type = 'geopython_workflow' AND is_builtin AND tenant_id IS NULL;

INSERT INTO system.permissions (permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status)
VALUES
    ('system.engine_raster_policy.read', 'system', 'read', 'low', false, ARRAY['platform','tenant']::text[], true, 'permissions.system.engine_raster_policy.read.name', 'permissions.system.engine_raster_policy.read.description', 'active'),
    ('system.engine_raster_policy.update', 'system', 'update', 'medium', false, ARRAY['platform','tenant']::text[], true, 'permissions.system.engine_raster_policy.update.name', 'permissions.system.engine_raster_policy.update.description', 'active'),
    ('system.engine_raster_policy_runtime.read', 'system', 'read', 'low', false, ARRAY['platform']::text[], false, 'permissions.system.engine_raster_policy_runtime.read.name', 'permissions.system.engine_raster_policy_runtime.read.description', 'active');
INSERT INTO system.role_permissions (role_id,permission_id,source_type)
SELECT role.id,permission.id,'product'
FROM system.roles role JOIN system.permissions permission ON
    (role.role_key IN ('platform.system_administrator','tenant.administrator','tenant.infrastructure_administrator')
        AND permission.permission_key IN ('system.engine_raster_policy.read','system.engine_raster_policy.update'))
    OR (role.role_key = 'platform.geopython_runtime' AND permission.permission_key = 'system.engine_raster_policy_runtime.read')
WHERE role.tenant_id IS NULL AND role.status = 'active'
ON CONFLICT DO NOTHING;

CREATE TEMP TABLE raster_policy_affected_principals ON COMMIT DROP AS
SELECT DISTINCT assignment.principal_id FROM system.role_assignments assignment
JOIN system.roles role ON role.id = assignment.role_id
WHERE assignment.status = 'active' AND role.tenant_id IS NULL
AND role.role_key IN ('platform.system_administrator','tenant.administrator','tenant.infrastructure_administrator','platform.geopython_runtime');
UPDATE system.principals principal SET authorization_version = principal.authorization_version + 1,updated_at = now()
FROM raster_policy_affected_principals affected WHERE principal.id = affected.principal_id;
UPDATE system.refresh_token_families family SET revoked_at = now(), revoked_reason = 'authorization_catalog_changed',updated_at = now()
FROM raster_policy_affected_principals affected WHERE family.principal_id = affected.principal_id AND family.revoked_at IS NULL;
COMMIT;
