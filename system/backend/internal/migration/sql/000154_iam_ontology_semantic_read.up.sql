BEGIN;

-- Definition consumption is independent from draft administration/publication.
-- Existing User and Service roles do not gain implicit access.
INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status
) VALUES (
    'ontology.semantic.read', 'ontology', 'read', 'low', true,
    ARRAY['tenant']::text[], true,
    'permissions.ontology.semantic.read.name', 'permissions.ontology.semantic.read.description', 'active'
);

COMMIT;
