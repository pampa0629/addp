BEGIN;
CREATE TABLE system.module_log_sources (
 instance_id varchar(100) PRIMARY KEY, module_name varchar(100) NOT NULL,
 role varchar(30) NOT NULL CHECK (role IN ('backend','worker','scheduler','ingress')),
 host_node_name varchar(100) NOT NULL,
 capture_started_at timestamptz NOT NULL, observed_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 CHECK (observed_at >= capture_started_at AND expires_at > observed_at)
);
CREATE INDEX idx_module_log_sources_expires ON system.module_log_sources(expires_at);
CREATE INDEX idx_module_log_sources_capture ON system.module_log_sources(capture_started_at DESC,instance_id DESC);
CREATE TABLE system.module_log_source_nodes (
 node varchar(100) PRIMARY KEY, boot_id varchar(100) NOT NULL,
 sequence bigint NOT NULL, sampled_at timestamptz NOT NULL,
 received_at timestamptz NOT NULL, complete boolean NOT NULL,
 payload_hash varchar(64) NOT NULL
);
CREATE TABLE system.module_log_source_boots (boot_id varchar(100) PRIMARY KEY, expires_at timestamptz NOT NULL);
CREATE INDEX idx_module_log_source_boots_expires ON system.module_log_source_boots(expires_at);
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES ('system.module_log_source.create','system','create','medium',false,ARRAY['platform']::text[],false,'permissions.system.module_log_source.create.name','permissions.system.module_log_source.create.description','active');
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key='system.module_log_source.create'
WHERE r.tenant_id IS NULL AND r.role_key='platform.log_observer_runtime';
CREATE TEMP TABLE log_source_affected_principals ON COMMIT DROP AS
    SELECT DISTINCT assignment.principal_id
    FROM system.role_assignments AS assignment
    JOIN system.roles AS role ON role.id = assignment.role_id
    WHERE assignment.status = 'active' AND role.tenant_id IS NULL
      AND role.role_key = 'platform.log_observer_runtime'
;
UPDATE system.principals AS principal
SET authorization_version = principal.authorization_version + 1, updated_at = now()
FROM log_source_affected_principals AS affected WHERE principal.id = affected.principal_id;

UPDATE system.refresh_token_families AS family
SET revoked_at = now(), revoked_reason = 'authorization_catalog_changed', updated_at = now()
FROM log_source_affected_principals AS affected
WHERE family.principal_id = affected.principal_id AND family.revoked_at IS NULL;

COMMIT;
