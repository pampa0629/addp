BEGIN;
ALTER TABLE system.execution_authorizations
    DROP CONSTRAINT execution_authorizations_audience_check,
    ADD CONSTRAINT execution_authorizations_audience_check
        CHECK (audience IN ('model','quality','develop','transfer','service','duckdb','ontology','manager','cleanup','spark'));

INSERT INTO system.role_permissions(role_id, permission_id, source_type)
SELECT r.id, p.id, 'product'
FROM system.roles r JOIN system.permissions p ON p.permission_key = 'system.execution_authorization.execute'
WHERE r.tenant_id IS NULL AND r.role_key = 'tenant.spark_runtime';

UPDATE system.refresh_token_families f
SET revoked_at = statement_timestamp(), revoked_reason = 'authorization_catalog_changed', updated_at = statement_timestamp()
WHERE f.revoked_at IS NULL AND EXISTS (
    SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id = a.role_id
    WHERE a.principal_id = f.principal_id AND a.status = 'active'
      AND r.tenant_id IS NULL AND r.role_key = 'tenant.spark_runtime'
);
COMMIT;
