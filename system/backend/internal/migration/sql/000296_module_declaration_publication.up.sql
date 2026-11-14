BEGIN;

ALTER TABLE system.module_definitions ADD COLUMN declaration_published_at timestamptz;

INSERT INTO system.permissions (permission_key, owner_module, action, risk_level, delegable,
 allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status)
VALUES ('system.module_declaration.publish','system','publish','medium',false,ARRAY['platform'],false,
 'permissions.system.module_declaration.publish.name','permissions.system.module_declaration.publish.description','active');

INSERT INTO system.roles (role_key,name_i18n_key,description_i18n_key,role_type,
 allowed_scope_types,allowed_principal_types,immutable,status)
VALUES ('platform.module_publisher','roles.platform.module_publisher.name','roles.platform.module_publisher.description',
 'platform_builtin',ARRAY['platform'],ARRAY['service_principal'],true,'active');

INSERT INTO system.role_permissions (role_id,permission_id,source_type)
SELECT r.id,p.id,'product' FROM system.roles r CROSS JOIN system.permissions p
WHERE r.role_key='platform.module_publisher' AND r.tenant_id IS NULL AND p.permission_key='system.module_declaration.publish';

DO $$
DECLARE
    module text;
    publisher text;
    principal bigint;
BEGIN
    FOREACH module IN ARRAY ARRAY['agent', 'asset', 'catalog', 'copilot', 'develop', 'gateway', 'graph', 'inference', 'manager', 'meta', 'model', 'monitor', 'ontology', 'orchestrator', 'portal', 'quality', 'security', 'service', 'standard', 'system', 'transfer', 'workbench'] LOOP
        publisher := 'addp-' || module || '-publisher';
        INSERT INTO system.principals (principal_type, status)
        VALUES ('service_principal','active') RETURNING id INTO principal;
        INSERT INTO system.service_principals (id,name,description,owner_scope,created_by_principal_id)
        VALUES (principal,publisher,module || ' declaration publisher','platform',principal);
        INSERT INTO system.oauth_clients (client_id,display_name,client_type,service_principal_id,
             redirect_uris,grant_types,response_types,allowed_scopes,allowed_audiences,token_endpoint_auth_method,status)
        VALUES (publisher,publisher,'confidential',principal,ARRAY[]::text[],ARRAY['client_credentials'],ARRAY[]::text[],
             ARRAY['addp.api'],ARRAY['addp.api'],'client_secret_basic','disabled');
        INSERT INTO system.role_assignments (principal_id,role_id,scope_type,status,valid_from,source_type,grant_reason)
        SELECT principal,id,'platform','active',now(),'bootstrap','Publish this module declaration'
        FROM system.roles WHERE tenant_id IS NULL AND role_key='platform.module_publisher';
    END LOOP;
END $$;

COMMIT;
