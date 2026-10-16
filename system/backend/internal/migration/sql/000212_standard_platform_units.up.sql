BEGIN;

-- Standard owns one unit resource contract in both verified AuthContext realms.
UPDATE system.permissions
SET allowed_scope_types=ARRAY['platform','tenant','department','project_group']::text[]
WHERE permission_key IN ('standard.unit.create','standard.unit.delete','standard.unit.read','standard.unit.update');

INSERT INTO system.role_permissions(role_id,permission_id,source_type)
SELECT r.id,p.id,'product' FROM system.roles r CROSS JOIN system.permissions p
WHERE r.tenant_id IS NULL AND r.role_key='platform.system_administrator'
AND p.permission_key IN ('standard.unit.create','standard.unit.delete','standard.unit.read','standard.unit.update')
ON CONFLICT(role_id,permission_id) DO NOTHING;

-- Existing role-permission triggers advance authorization_version.
UPDATE system.refresh_token_families f
SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
WHERE f.revoked_at IS NULL AND EXISTS(
 SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
 WHERE a.principal_id=f.principal_id AND a.status='active'
 AND r.tenant_id IS NULL AND r.role_key='platform.system_administrator'
);
COMMIT;
