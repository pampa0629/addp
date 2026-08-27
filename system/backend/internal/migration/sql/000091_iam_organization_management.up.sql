BEGIN;

ALTER TABLE system.departments
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);
ALTER TABLE system.department_memberships
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);
ALTER TABLE system.project_groups
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);
ALTER TABLE system.project_group_memberships
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);

ALTER TABLE system.department_memberships
    DROP CONSTRAINT department_memberships_department_id_tenant_membership_id_key;
CREATE UNIQUE INDEX uq_department_memberships_active_identity
    ON system.department_memberships (department_id, tenant_membership_id)
    WHERE status = 'active';

ALTER TABLE system.project_group_memberships
    DROP CONSTRAINT project_group_memberships_project_group_id_tenant_membershi_key;
CREATE UNIQUE INDEX uq_project_group_memberships_active_identity
    ON system.project_group_memberships (project_group_id, tenant_membership_id)
    WHERE status = 'active';

UPDATE system.permissions
SET status = 'active'
WHERE permission_key IN (
    'iam.department.create',
    'iam.department.read',
    'iam.department.update',
    'iam.department_membership.close',
    'iam.department_membership.create',
    'iam.department_membership.read',
    'iam.department_membership.update',
    'iam.project_group.close',
    'iam.project_group.create',
    'iam.project_group.read',
    'iam.project_group.update',
    'iam.project_group_membership.close',
    'iam.project_group_membership.create',
    'iam.project_group_membership.read',
    'iam.project_group_membership.update'
)
  AND status = 'disabled';

UPDATE system.permissions
SET status = 'disabled'
WHERE permission_key = 'iam.department.delete'
  AND status = 'active';

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable, allowed_scope_types,
    tenant_customizable, name_i18n_key, description_i18n_key, status
) VALUES
    ('iam.department.restore', 'system', 'restore', 'high', false, ARRAY['tenant']::text[], false,
     'permissions.iam.department.restore.name', 'permissions.iam.department.restore.description', 'active')
ON CONFLICT (permission_key) DO NOTHING;

DELETE FROM system.role_permissions role_permission
USING system.permissions permission
WHERE role_permission.permission_id = permission.id
  AND permission.permission_key = 'iam.department.delete';

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key IN (
      'iam.department.create',
      'iam.department.read',
      'iam.department.restore',
      'iam.department.update',
      'iam.department_membership.close',
      'iam.department_membership.create',
      'iam.department_membership.read',
      'iam.department_membership.update',
      'iam.project_group.close',
      'iam.project_group.create',
      'iam.project_group.read',
      'iam.project_group.update',
      'iam.project_group_membership.close',
      'iam.project_group_membership.create',
      'iam.project_group_membership.read',
      'iam.project_group_membership.update'
  )
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.administrator'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
