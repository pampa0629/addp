BEGIN;
CREATE TEMP TABLE quality_rule_affected_principals ON COMMIT DROP AS
SELECT DISTINCT a.principal_id FROM system.role_assignments a
JOIN system.role_permissions b ON b.role_id=a.role_id
JOIN system.permissions p ON p.id=b.permission_id
WHERE a.status='active' AND p.permission_key IN ('quality.plan.create','quality.plan.read','quality.plan.update','quality.plan.delete');

INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
SELECT replace(permission_key,'quality.plan.','quality.rule.'),owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,
replace(name_i18n_key,'quality.plan.','quality.rule.'),replace(description_i18n_key,'quality.plan.','quality.rule.'),'active'
FROM system.permissions WHERE permission_key IN ('quality.plan.create','quality.plan.read','quality.plan.update','quality.plan.delete');

INSERT INTO system.role_permissions(role_id,permission_id,source_type,created_by_principal_id)
SELECT DISTINCT ON (b.role_id,n.id) b.role_id,n.id,b.source_type,b.created_by_principal_id
FROM system.role_permissions b
JOIN system.permissions p ON p.id=b.permission_id
JOIN system.permissions n ON n.permission_key='quality.rule.'||p.action
WHERE p.permission_key IN ('quality.plan.create','quality.plan.read','quality.plan.update','quality.plan.delete')
ORDER BY b.role_id,n.id,p.id ON CONFLICT(role_id,permission_id) DO NOTHING;

-- Editing plans requires seeing rule candidates, but never grants rule execution.
INSERT INTO system.role_permissions(role_id,permission_id,source_type,created_by_principal_id)
SELECT DISTINCT ON (b.role_id,n.id) b.role_id,n.id,b.source_type,b.created_by_principal_id
FROM system.role_permissions b JOIN system.permissions p ON p.id=b.permission_id
CROSS JOIN system.permissions n
WHERE p.permission_key IN ('quality.plan.create','quality.plan.update') AND n.permission_key='quality.rule.read'
ORDER BY b.role_id,n.id,p.id ON CONFLICT(role_id,permission_id) DO NOTHING;

UPDATE system.principals p SET authorization_version=authorization_version+1,updated_at=now()
FROM quality_rule_affected_principals a WHERE p.id=a.principal_id;
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
FROM quality_rule_affected_principals a WHERE f.principal_id=a.principal_id AND f.revoked_at IS NULL;
COMMIT;
