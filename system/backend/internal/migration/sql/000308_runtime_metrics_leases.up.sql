BEGIN;
ALTER TABLE system.geopython_metrics_lease RENAME TO runtime_metrics_leases;
ALTER TABLE system.runtime_metrics_leases DROP COLUMN singleton;
ALTER TABLE system.runtime_metrics_leases ADD PRIMARY KEY (engine_id);
DROP INDEX system.idx_geopython_metrics_lease_engine_id;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable, allowed_scope_types,
    tenant_customizable, name_i18n_key, description_i18n_key, status
) VALUES ('system.runtime_metrics.update','system','update','medium',false,ARRAY['platform']::text[],
    false,'permissions.system.runtime_metrics.update.name','permissions.system.runtime_metrics.update.description','active');

DO $$
DECLARE product text;
DECLARE client_name text;
DECLARE runtime_role text;
DECLARE service_id bigint;
BEGIN
  FOREACH product IN ARRAY ARRAY['jupyter','supermap'] LOOP
    client_name := 'addp-' || product;
    runtime_role := 'platform.' || product || '_runtime';
    INSERT INTO system.roles (role_key, name_i18n_key, description_i18n_key,role_type,
        allowed_scope_types,allowed_principal_types,immutable,status)
    VALUES(runtime_role,'roles.'||runtime_role||'.name','roles.'||runtime_role||'.description','platform_builtin',
        ARRAY['platform']::text[],ARRAY['service_principal']::text[],true,'active');
    INSERT INTO system.principals (principal_type,status) VALUES ('service_principal','active') RETURNING id INTO service_id;
    INSERT INTO system.service_principals (id,name,description,owner_scope,created_by_principal_id)
    VALUES(service_id,client_name,product||' metrics lease','platform',service_id);
    INSERT INTO system.oauth_clients (client_id,display_name,client_type,client_secret_hash,service_principal_id,redirect_uris,
        grant_types,response_types,allowed_scopes,allowed_audiences,token_endpoint_auth_method,status)
    VALUES(client_name,product||' metrics lease','confidential',NULL,service_id,ARRAY[]::text[],ARRAY['client_credentials']::text[],
           ARRAY[]::text[],ARRAY['addp.api']::text[],ARRAY['addp.api']::text[],'client_secret_basic','disabled');
    INSERT INTO system.role_assignments(principal_id,role_id,scope_type,status,valid_from,source_type,grant_reason)
    SELECT service_id,id,'platform','active',transaction_timestamp(),'bootstrap','own runtime metrics lease'
    FROM system.roles WHERE tenant_id IS NULL AND role_key=runtime_role;
  END LOOP;
END;
$$;

INSERT INTO system.role_permissions(role_id,permission_id,source_type)
SELECT role.id,permission.id,'product' FROM system.roles role CROSS JOIN system.permissions permission
WHERE role.tenant_id IS NULL AND permission.permission_key='system.runtime_metrics.update'
AND role.role_key IN ('platform.document_runtime','platform.duckdb_runtime','platform.geopython_runtime','platform.inference_runtime','platform.jupyter_runtime','platform.model3d_runtime','platform.pointcloud_runtime','platform.spark_runtime','platform.supermap_runtime');
COMMIT;
