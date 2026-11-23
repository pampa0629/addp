BEGIN;

ALTER TABLE system.engines ADD COLUMN runtime_registrar VARCHAR(96);

DELETE FROM system.role_permissions
WHERE role_id = (SELECT id FROM system.roles WHERE tenant_id IS NULL AND role_key = 'tenant.spark_runtime')
  AND permission_id = (SELECT id FROM system.permissions WHERE permission_key = 'system.engine.read')
  AND source_type = 'product';

COMMIT;
