BEGIN;

INSERT INTO system.permissions (
    permission_key,
    owner_module,
    action,
    risk_level,
    delegable,
    allowed_scope_types,
    tenant_customizable,
    name_i18n_key,
    description_i18n_key,
    status
) VALUES
    ('monitor.alert_incident.read', 'monitor', 'read', 'low', false, ARRAY['tenant']::text[], true, 'permissions.monitor.alert_incident.read.name', 'permissions.monitor.alert_incident.read.description', 'active'),
    ('monitor.alert_incident.update', 'monitor', 'update', 'medium', false, ARRAY['tenant']::text[], true, 'permissions.monitor.alert_incident.update.name', 'permissions.monitor.alert_incident.update.description', 'active'),
    ('monitor.alert_rule.create', 'monitor', 'create', 'medium', false, ARRAY['tenant']::text[], true, 'permissions.monitor.alert_rule.create.name', 'permissions.monitor.alert_rule.create.description', 'active'),
    ('monitor.alert_rule.delete', 'monitor', 'delete', 'high', false, ARRAY['tenant']::text[], true, 'permissions.monitor.alert_rule.delete.name', 'permissions.monitor.alert_rule.delete.description', 'active'),
    ('monitor.alert_rule.read', 'monitor', 'read', 'low', false, ARRAY['tenant']::text[], true, 'permissions.monitor.alert_rule.read.name', 'permissions.monitor.alert_rule.read.description', 'active'),
    ('monitor.alert_rule.update', 'monitor', 'update', 'medium', false, ARRAY['tenant']::text[], true, 'permissions.monitor.alert_rule.update.name', 'permissions.monitor.alert_rule.update.description', 'active'),
    ('monitor.notification_delivery.read', 'monitor', 'read', 'low', false, ARRAY['tenant']::text[], true, 'permissions.monitor.notification_delivery.read.name', 'permissions.monitor.notification_delivery.read.description', 'active'),
    ('monitor.notification_delivery.retry', 'monitor', 'retry', 'medium', false, ARRAY['tenant']::text[], true, 'permissions.monitor.notification_delivery.retry.name', 'permissions.monitor.notification_delivery.retry.description', 'active'),
    ('monitor.notification_destination.create', 'monitor', 'create', 'medium', false, ARRAY['tenant']::text[], true, 'permissions.monitor.notification_destination.create.name', 'permissions.monitor.notification_destination.create.description', 'active'),
    ('monitor.notification_destination.delete', 'monitor', 'delete', 'high', false, ARRAY['tenant']::text[], true, 'permissions.monitor.notification_destination.delete.name', 'permissions.monitor.notification_destination.delete.description', 'active'),
    ('monitor.notification_destination.execute', 'monitor', 'execute', 'medium', false, ARRAY['tenant']::text[], true, 'permissions.monitor.notification_destination.execute.name', 'permissions.monitor.notification_destination.execute.description', 'active'),
    ('monitor.notification_destination.read', 'monitor', 'read', 'low', false, ARRAY['tenant']::text[], true, 'permissions.monitor.notification_destination.read.name', 'permissions.monitor.notification_destination.read.description', 'active'),
    ('monitor.notification_destination.update', 'monitor', 'update', 'medium', false, ARRAY['tenant']::text[], true, 'permissions.monitor.notification_destination.update.name', 'permissions.monitor.notification_destination.update.description', 'active');

INSERT INTO system.roles (
    tenant_id,
    role_key,
    name,
    description,
    name_i18n_key,
    description_i18n_key,
    role_type,
    allowed_scope_types,
    allowed_principal_types,
    immutable,
    status,
    created_by_principal_id
) VALUES (
    NULL,
    'tenant.monitoring_operator',
    NULL,
    NULL,
    'roles.tenant.monitoring_operator.name',
    'roles.tenant.monitoring_operator.description',
    'tenant_builtin',
    ARRAY['tenant']::text[],
    ARRAY['user']::text[],
    true,
    'active',
    NULL
);

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'monitor.alert_incident.read',
      'monitor.alert_incident.update',
      'monitor.alert_rule.create',
      'monitor.alert_rule.delete',
      'monitor.alert_rule.read',
      'monitor.alert_rule.update',
      'monitor.execution.read',
      'monitor.health.read',
      'monitor.notification_delivery.read',
      'monitor.notification_delivery.retry',
      'monitor.notification_destination.create',
      'monitor.notification_destination.delete',
      'monitor.notification_destination.execute',
      'monitor.notification_destination.read',
      'monitor.notification_destination.update',
      'monitor.statistics.read'
  )
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.monitoring_operator'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active';

COMMIT;
