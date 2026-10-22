BEGIN;

CREATE TABLE system.meta_collection_qualifications (
 id uuid PRIMARY KEY CHECK(id <> '00000000-0000-0000-0000-000000000000'),
 tenant_id bigint NOT NULL REFERENCES system.tenants(id),
 engine_id bigint NOT NULL,
 task_id bigint NOT NULL CHECK(task_id>0),
 task_definition_digest text NOT NULL CHECK(task_definition_digest ~ '^[0-9a-f]{64}$'),
 engine_version bigint NOT NULL CHECK(engine_version>0),
 scan_depth text NOT NULL CHECK(scan_depth='basic'),
 scope_kind text NOT NULL CHECK(scope_kind='engine'),
 purpose text NOT NULL CHECK(purpose=btrim(purpose) AND length(purpose) BETWEEN 1 AND 2000),
 status text NOT NULL CHECK(status IN('active','revoked')),
 version bigint NOT NULL CHECK(version>0),
 granted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL CHECK(isfinite(expires_at) AND expires_at>granted_at),
 granted_by_principal_id bigint NOT NULL REFERENCES system.principals(id),
 granted_by_membership_id bigint NOT NULL,
 granted_authorization_version bigint NOT NULL CHECK(granted_authorization_version>0),
 revoked_at timestamptz,
 revoked_by_principal_id bigint REFERENCES system.principals(id),
 revoked_reason text,
 FOREIGN KEY(tenant_id,engine_id) REFERENCES system.engines(tenant_id,id),
 FOREIGN KEY(tenant_id,granted_by_membership_id) REFERENCES system.tenant_memberships(tenant_id,id),
 CHECK((status='active' AND version=1 AND revoked_at IS NULL AND revoked_by_principal_id IS NULL AND revoked_reason IS NULL)
   OR (status='revoked' AND version=2 AND revoked_at IS NOT NULL AND isfinite(revoked_at)
       AND revoked_at>=granted_at AND revoked_at<expires_at AND revoked_by_principal_id IS NOT NULL
       AND revoked_reason IS NOT NULL AND revoked_reason=btrim(revoked_reason) AND length(revoked_reason) BETWEEN 1 AND 2000))
);
CREATE INDEX meta_collection_qualification_engine ON system.meta_collection_qualifications(tenant_id,engine_id,task_id);
CREATE INDEX meta_collection_qualification_approver ON system.meta_collection_qualifications(granted_by_principal_id);
CREATE INDEX meta_collection_qualification_membership ON system.meta_collection_qualifications(tenant_id,granted_by_membership_id);
CREATE INDEX meta_collection_qualification_revoker ON system.meta_collection_qualifications(revoked_by_principal_id);

CREATE FUNCTION system.guard_meta_collection_qualification_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'active' OR NEW.expires_at<=clock_timestamp() THEN
   RAISE EXCEPTION 'collection qualification expiry must be future' USING ERRCODE='23514', CONSTRAINT='meta_collection_qualification_expiry';
  END IF;
  PERFORM pg_advisory_xact_lock(hashtextextended(concat_ws('|','meta-collection',NEW.tenant_id,NEW.engine_id,NEW.task_id),0));
  IF EXISTS(SELECT 1 FROM system.meta_collection_qualifications q
    WHERE q.tenant_id=NEW.tenant_id AND q.engine_id=NEW.engine_id AND q.task_id=NEW.task_id AND q.status='active'
     AND tstzrange(q.granted_at,q.expires_at,'[)') && tstzrange(NEW.granted_at,NEW.expires_at,'[)')) THEN
   RAISE EXCEPTION 'overlapping collection qualification' USING ERRCODE='23505', CONSTRAINT='meta_collection_qualification_overlap';
  END IF;
  RETURN NEW;
 END IF;
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'collection qualification history is immutable' USING ERRCODE='23514'; END IF;
 IF OLD.status<>'active' OR OLD.expires_at<=clock_timestamp()
   OR NEW.status<>'revoked' OR NEW.version<>OLD.version+1
   OR (to_jsonb(NEW)-ARRAY['status','version','revoked_at','revoked_by_principal_id','revoked_reason'])
       IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['status','version','revoked_at','revoked_by_principal_id','revoked_reason']) THEN
  RAISE EXCEPTION 'collection qualification permits only current explicit revocation' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER meta_collection_qualification_history BEFORE INSERT OR UPDATE OR DELETE ON system.meta_collection_qualifications
 FOR EACH ROW EXECUTE FUNCTION system.guard_meta_collection_qualification_history();

INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status) VALUES
 ('system.meta_collection_qualification.approve','system','approve','high',false,ARRAY['tenant']::text[],true,'permissions.system.meta_collection_qualification.approve.name','permissions.system.meta_collection_qualification.approve.description','active'),
 ('system.meta_collection_qualification.read','system','read','low',false,ARRAY['tenant']::text[],true,'permissions.system.meta_collection_qualification.read.name','permissions.system.meta_collection_qualification.read.description','active'),
 ('system.meta_collection_qualification.revoke','system','revoke','high',false,ARRAY['tenant']::text[],true,'permissions.system.meta_collection_qualification.revoke.name','permissions.system.meta_collection_qualification.revoke.description','active'),
 ('system.meta_collection_qualification.execute','system','execute','high',false,ARRAY['tenant']::text[],false,'permissions.system.meta_collection_qualification.execute.name','permissions.system.meta_collection_qualification.execute.description','active');
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
 SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON
 (r.role_key='tenant.engine_access_delegation_administrator' AND p.permission_key IN('system.meta_collection_qualification.approve','system.meta_collection_qualification.read','system.meta_collection_qualification.revoke'))
 OR (r.role_key='tenant.meta_runtime' AND p.permission_key='system.meta_collection_qualification.execute')
 OR (r.role_key='tenant.system_runtime' AND p.permission_key='meta.task_provider.read')
 WHERE r.tenant_id IS NULL;
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
 WHERE f.revoked_at IS NULL AND EXISTS(SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
 WHERE a.principal_id=f.principal_id AND a.status='active' AND r.tenant_id IS NULL AND r.role_key IN('tenant.engine_access_delegation_administrator','tenant.meta_runtime','tenant.system_runtime'));

COMMIT;
