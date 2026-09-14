BEGIN;

-- Invalidate authorizations before removing retired role/permission facts.
CREATE TEMP TABLE retired_collection_affected_principals ON COMMIT DROP AS
    SELECT DISTINCT assignment.principal_id
    FROM system.role_assignments assignment
    JOIN system.roles role ON role.id = assignment.role_id
    JOIN system.role_permissions binding ON binding.role_id = role.id
    JOIN system.permissions permission ON permission.id = binding.permission_id
    WHERE assignment.status = 'active'
      AND (
          permission.permission_key IN (
              'standard.collection.create', 'standard.collection.delete',
              'standard.collection.publish', 'standard.collection.read',
              'standard.collection.update', 'standard.collection_assignment.update'
          )
          OR (role.role_key = 'tenant.standard_runtime' AND role.tenant_id IS NULL
              AND permission.permission_key = 'iam.tenant_membership.read')
      );

UPDATE system.principals principal
SET authorization_version = principal.authorization_version + 1, updated_at = now()
FROM retired_collection_affected_principals affected
WHERE principal.id = affected.principal_id;

DELETE FROM system.role_permissions binding
USING system.permissions permission, system.roles role
WHERE binding.permission_id = permission.id AND binding.role_id = role.id
  AND (
      permission.permission_key IN (
          'standard.collection.create', 'standard.collection.delete',
          'standard.collection.publish', 'standard.collection.read',
          'standard.collection.update', 'standard.collection_assignment.update'
      )
      OR (role.role_key = 'tenant.standard_runtime' AND role.tenant_id IS NULL
          AND permission.permission_key = 'iam.tenant_membership.read')
  );

UPDATE system.permissions
SET status = 'disabled', updated_at = now()
WHERE permission_key IN (
    'standard.collection.create', 'standard.collection.delete',
    'standard.collection.publish', 'standard.collection.read',
    'standard.collection.update', 'standard.collection_assignment.update'
);

UPDATE system.refresh_token_families family
SET revoked_at = now(), revoked_reason = 'authorization_catalog_changed', updated_at = now()
FROM retired_collection_affected_principals affected
WHERE family.principal_id = affected.principal_id AND family.revoked_at IS NULL;

COMMIT;
