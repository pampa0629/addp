BEGIN;

ALTER TABLE system.tenants
    ADD COLUMN initialized_at timestamptz,
    ADD COLUMN initialized_by_principal_id bigint REFERENCES system.principals(id),
    ADD CONSTRAINT tenants_initialization_fact_complete CHECK (
        (initialized_at IS NULL) = (initialized_by_principal_id IS NULL)
    );

CREATE INDEX idx_tenants_initialized_by
    ON system.tenants (initialized_by_principal_id)
    WHERE initialized_by_principal_id IS NOT NULL;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES (
    'platform.tenant.initialize', 'system', 'initialize', 'high', false,
    ARRAY['platform']::text[], false,
    'permissions.platform.tenant.initialize.name',
    'permissions.platform.tenant.initialize.description', 'active'
);

UPDATE system.permissions
SET status = 'active', updated_at = now()
WHERE permission_key IN (
    'iam.tenant_role.create',
    'iam.tenant_role.delete',
    'iam.tenant_role.read',
    'iam.tenant_role.update',
    'iam.tenant_role_assignment.create',
    'iam.tenant_role_assignment.read',
    'iam.tenant_role_assignment.revoke'
);

DO $$
BEGIN
    IF (
        SELECT count(*)
        FROM system.permissions
        WHERE permission_key IN (
            'iam.tenant_role.create',
            'iam.tenant_role.delete',
            'iam.tenant_role.read',
            'iam.tenant_role.update',
            'iam.tenant_role_assignment.create',
            'iam.tenant_role_assignment.read',
            'iam.tenant_role_assignment.revoke',
            'platform.tenant.initialize'
        ) AND status = 'active'
    ) <> 8 THEN
        RAISE EXCEPTION 'tenant administration closure requires eight active permissions';
    END IF;
END;
$$;

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM (VALUES
    ('platform.system_administrator', 'platform.tenant.initialize'),
    ('tenant.administrator', 'iam.tenant_role.create'),
    ('tenant.administrator', 'iam.tenant_role.delete'),
    ('tenant.administrator', 'iam.tenant_role.read'),
    ('tenant.administrator', 'iam.tenant_role.update'),
    ('tenant.administrator', 'iam.tenant_role_assignment.create'),
    ('tenant.administrator', 'iam.tenant_role_assignment.read'),
    ('tenant.administrator', 'iam.tenant_role_assignment.revoke')
) AS seed(role_key, permission_key)
JOIN system.roles role
  ON role.tenant_id IS NULL AND role.role_key = seed.role_key
JOIN system.permissions permission
  ON permission.permission_key = seed.permission_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

CREATE FUNCTION system.has_stable_tenant_administrator(target_tenant_id bigint)
RETURNS boolean
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM system.role_assignments assignment
        JOIN system.roles role ON role.id = assignment.role_id
        JOIN system.principals principal ON principal.id = assignment.principal_id
        JOIN system.tenant_memberships membership
          ON membership.tenant_id = assignment.tenant_id
         AND membership.principal_id = assignment.principal_id
        WHERE assignment.tenant_id = target_tenant_id
          AND assignment.scope_type = 'tenant'
          AND assignment.status = 'active'
          AND assignment.valid_from <= now()
          AND assignment.valid_until IS NULL
          AND role.tenant_id IS NULL
          AND role.role_key = 'tenant.administrator'
          AND role.status = 'active'
          AND principal.status = 'active'
          AND membership.status = 'active'
          AND membership.expires_at IS NULL
    );
$$;

CREATE FUNCTION system.require_tenant_administrator()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    affected_tenant_id bigint;
BEGIN
    IF TG_TABLE_NAME = 'tenants' THEN
        affected_tenant_id := COALESCE(NEW.id, OLD.id);
    ELSE
        affected_tenant_id := COALESCE(NEW.tenant_id, OLD.tenant_id);
    END IF;

    IF EXISTS (
        SELECT 1
        FROM system.tenants tenant
        WHERE tenant.id = affected_tenant_id
          AND tenant.status <> 'closed'
          AND tenant.initialized_at IS NOT NULL
    ) AND NOT system.has_stable_tenant_administrator(affected_tenant_id) THEN
        RAISE EXCEPTION 'tenant must retain an effective tenant administrator'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION system.require_all_tenant_administrators()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM system.tenants tenant
        WHERE tenant.status <> 'closed'
          AND tenant.initialized_at IS NOT NULL
          AND NOT system.has_stable_tenant_administrator(tenant.id)
    ) THEN
        RAISE EXCEPTION 'every initialized tenant must retain an effective tenant administrator'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE CONSTRAINT TRIGGER trg_tenants_require_administrator
AFTER INSERT OR UPDATE ON system.tenants
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION system.require_tenant_administrator();

CREATE CONSTRAINT TRIGGER trg_tenant_memberships_require_administrator
AFTER INSERT OR UPDATE OR DELETE ON system.tenant_memberships
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION system.require_tenant_administrator();

CREATE CONSTRAINT TRIGGER trg_role_assignments_require_tenant_administrator
AFTER INSERT OR UPDATE OR DELETE ON system.role_assignments
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION system.require_tenant_administrator();

CREATE CONSTRAINT TRIGGER trg_principals_require_tenant_administrator
AFTER UPDATE OF status ON system.principals
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION system.require_all_tenant_administrators();

CREATE CONSTRAINT TRIGGER trg_roles_require_tenant_administrator
AFTER UPDATE OF status ON system.roles
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION system.require_all_tenant_administrators();

COMMIT;
