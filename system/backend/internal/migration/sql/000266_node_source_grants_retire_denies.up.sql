BEGIN;

-- One immutable rule store. Null means a precise DataItem; a non-null boolean
-- explicitly selects a persistent node scope, including false for direct items.
ALTER TABLE system.engine_access_grants ADD COLUMN node_recursive boolean;

CREATE FUNCTION system.valid_source_node_path(path jsonb, engine bigint)
RETURNS boolean LANGUAGE sql IMMUTABLE STRICT SET search_path=pg_catalog AS $$
 SELECT CASE WHEN jsonb_typeof(path->'segments')='array' AND jsonb_array_length(path->'segments') IN (1,2)
 THEN engine > 0 AND path = jsonb_build_object(
  'version','catalog.path/v1','engine_id',engine,'segments',
  jsonb_build_array(jsonb_build_object('term','server','kind','server','name','')) ||
  CASE WHEN jsonb_array_length(path->'segments')=2 THEN
   jsonb_build_array(jsonb_build_object('term','schema','kind','namespace','name',path->'segments'->1->>'name'))
  ELSE '[]'::jsonb END)
  AND (jsonb_array_length(path->'segments')=1 OR
   (length(path->'segments'->1->>'name')>0 AND octet_length(path->'segments'->1->>'name')<=512))
 ELSE false END IS TRUE
$$;

-- Segment equality, never name prefix matching. Current trusted DataItem
-- bindings remain necessary; a scope does not authorize an unrecognized table.
CREATE FUNCTION system.source_node_covers(node jsonb, recursive boolean, target jsonb)
RETURNS boolean LANGUAGE sql IMMUTABLE STRICT SET search_path=pg_catalog AS $$
 SELECT CASE WHEN system.valid_source_node_path(node,(node->>'engine_id')::bigint)
  AND jsonb_typeof(target->'segments')='array' AND jsonb_array_length(target->'segments')=3
 THEN node->'engine_id'=target->'engine_id' AND target->>'version'='catalog.path/v1'
  AND target->'segments'->0 = node->'segments'->0
  AND target->'segments'->1->>'term'='schema' AND target->'segments'->1->>'kind'='namespace'
  AND target->'segments'->2->>'term'='table' AND target->'segments'->2->>'kind'='table'
  AND (recursive OR jsonb_array_length(node->'segments')=2)
  AND (jsonb_array_length(node->'segments')=1 OR target->'segments'->1=node->'segments'->1)
 ELSE false END IS TRUE
$$;

ALTER TABLE system.engine_access_grants
 DROP CONSTRAINT source_grant_dataitem_identity,
 DROP CONSTRAINT source_grant_boundary_hash,
 ADD CONSTRAINT source_grant_identity CHECK ((CASE WHEN node_recursive IS NULL THEN
  system.valid_source_single_identity(catalog_path,engine_id,item_fingerprint) AND boundary_hash ~ '^[0-9a-f]{64}$'
 ELSE approval_mode='independent' AND item_fingerprint='' AND boundary_hash=''
  AND file_composition IS NULL AND scope_boundary IS NULL AND system.valid_source_node_path(catalog_path,engine_id)
 END) IS TRUE);

CREATE FUNCTION system.guard_node_source_grant() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.node_recursive IS NOT NULL AND NOT EXISTS (SELECT 1 FROM system.engines e
  WHERE e.tenant_id=NEW.tenant_id AND e.id=NEW.engine_id AND e.engine_type='postgresql') THEN
  RAISE EXCEPTION 'node scopes require an admitted PostgreSQL engine'
   USING ERRCODE='23514',CONSTRAINT='source_grant_node_engine';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER trg_source_grant_node BEFORE INSERT ON system.engine_access_grants
 FOR EACH ROW EXECUTE FUNCTION system.guard_node_source_grant();

CREATE OR REPLACE FUNCTION system.guard_source_grant_relation() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('source_grant_relation|' || jsonb_build_array(
  NEW.tenant_id,NEW.engine_id,NEW.catalog_path,NEW.node_recursive,NEW.recipient_type,NEW.recipient_id,NEW.action)::text,0));
 IF EXISTS (SELECT 1 FROM system.engine_access_grants g WHERE g.request_id<>NEW.request_id
  AND g.tenant_id=NEW.tenant_id AND g.engine_id=NEW.engine_id AND g.catalog_path=NEW.catalog_path
  AND g.node_recursive IS NOT DISTINCT FROM NEW.node_recursive AND g.recipient_type=NEW.recipient_type
  AND g.recipient_id=NEW.recipient_id AND g.action=NEW.action AND g.granted_at<=clock_timestamp()
  AND (g.expires_at IS NULL OR g.expires_at>clock_timestamp())
  AND NOT EXISTS(SELECT 1 FROM system.engine_access_grant_revocations r WHERE r.request_id=g.request_id)) THEN
  RAISE EXCEPTION 'source authorization relation already exists; withdraw before changing expiry'
   USING ERRCODE='23505',CONSTRAINT='engine_access_grant_relation_exists';
 END IF;
 RETURN NEW;
END;
$$;
CREATE INDEX idx_source_grant_node ON system.engine_access_grants(tenant_id,engine_id)
 WHERE node_recursive IS NOT NULL;

-- Source Deny is retired completely. Generic audit and other domains' policies
-- are unaffected. Historical migrations remain upgrade input, not runtime paths.
DROP TABLE system.engine_access_deny_releases;
DROP TABLE system.engine_access_denies;
DROP FUNCTION system.guard_engine_access_deny_release_insert();
DROP FUNCTION system.preserve_engine_access_deny_release();
DROP FUNCTION system.guard_engine_access_deny_insert();
DROP FUNCTION system.preserve_engine_access_deny();
CREATE TEMP TABLE retired_source_deny_principals ON COMMIT DROP AS
 SELECT DISTINCT a.principal_id FROM system.role_assignments a
 JOIN system.role_permissions rp ON rp.role_id=a.role_id JOIN system.permissions p ON p.id=rp.permission_id
 WHERE a.status='active' AND p.permission_key IN ('system.engine_access_deny.create','system.engine_access_deny.release');
DELETE FROM system.role_permissions rp USING system.permissions p WHERE rp.permission_id=p.id
 AND p.permission_key IN ('system.engine_access_deny.create','system.engine_access_deny.release');
UPDATE system.permissions SET status='disabled',tenant_customizable=false,updated_at=now()
 WHERE permission_key IN ('system.engine_access_deny.create','system.engine_access_deny.release');
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
 FROM retired_source_deny_principals a WHERE f.principal_id=a.principal_id AND f.revoked_at IS NULL;

COMMIT;
