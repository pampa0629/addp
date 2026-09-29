BEGIN;

ALTER TABLE system.module_runtime_instances
    DROP CONSTRAINT module_runtime_instances_role_check,
    ADD CONSTRAINT module_runtime_instances_role_check
        CHECK (role IN ('backend', 'worker', 'scheduler', 'ingress')),
    ADD COLUMN process_started_at timestamptz,
    ADD COLUMN stopped_at timestamptz,
    ADD COLUMN stop_reason varchar(30) NOT NULL DEFAULT '',
    ADD CONSTRAINT module_runtime_instances_stop_reason_check
        CHECK (stop_reason IN ('', 'graceful', 'lease_expired'));

UPDATE system.module_definitions
SET enabled = true, version = version + 1, updated_at = now()
WHERE module_name = 'gateway' AND NOT enabled;

ALTER TABLE system.module_definitions
    DROP CONSTRAINT module_definitions_system_enabled,
    ADD CONSTRAINT module_definitions_bootstrap_enabled
        CHECK (module_name NOT IN ('system', 'gateway') OR enabled);

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'system.runtime_registry.update'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'platform.gateway_runtime'
  AND role.role_type = 'platform_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

WITH affected AS (
    SELECT DISTINCT assignment.principal_id
    FROM system.role_assignments AS assignment
    JOIN system.roles AS role ON role.id = assignment.role_id
    WHERE assignment.status = 'active'
      AND role.tenant_id IS NULL
      AND role.role_key = 'platform.gateway_runtime'
)
UPDATE system.principals AS principal
SET authorization_version = principal.authorization_version + 1,
    updated_at = now()
FROM affected
WHERE principal.id = affected.principal_id;

COMMIT;
