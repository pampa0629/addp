BEGIN;

-- Migration 207 is already applied and must retain its original bytes.
-- Correct the typed schema kind and validate the structural root forward.
CREATE OR REPLACE FUNCTION system.valid_source_single_identity(path jsonb, engine bigint, fingerprint text)
RETURNS boolean LANGUAGE sql IMMUTABLE STRICT SET search_path = pg_catalog AS $$
 SELECT (jsonb_typeof(path) = 'object' AND path->>'version' = 'catalog.path/v1'
 AND path->>'engine_id' = engine::text AND engine > 0
 AND jsonb_typeof(path->'segments') = 'array' AND jsonb_array_length(path->'segments') = 3
 AND path->'segments'->0->>'term' = 'server' AND path->'segments'->0->>'kind' = 'server'
 AND path->'segments'->0->>'name' = ''
 AND path->'segments'->1->>'term' = 'schema' AND path->'segments'->1->>'kind' = 'namespace'
 AND path->'segments'->2->>'term' = 'table' AND path->'segments'->2->>'kind' = 'table'
 AND length(path->'segments'->1->>'name') > 0 AND length(path->'segments'->2->>'name') > 0
 AND fingerprint ~ '^[0-9a-f]{64}$'
 AND fingerprint = encode(sha256(convert_to(engine::text || ':' ||
   (path->'segments'->1->>'name') || '.' || (path->'segments'->2->>'name'), 'UTF8')), 'hex')) IS TRUE
$$;

-- Revalidate persisted facts under the corrected function, not only new rows.
-- Invalid history blocks the migration; never rewrite immutable authority.
ALTER TABLE system.engine_access_grants
 DROP CONSTRAINT source_grant_dataitem_identity,
 ADD CONSTRAINT source_grant_dataitem_identity CHECK (system.valid_source_single_identity(catalog_path, engine_id, item_fingerprint));
ALTER TABLE system.engine_access_approval_requirements
 DROP CONSTRAINT source_approval_dataitem_identity,
 ADD CONSTRAINT source_approval_dataitem_identity CHECK (system.valid_source_single_identity(catalog_path, engine_id, item_fingerprint));
ALTER TABLE system.engine_access_denies
 DROP CONSTRAINT source_deny_dataitem_identity,
 ADD CONSTRAINT source_deny_dataitem_identity CHECK (system.valid_source_single_identity(catalog_path, engine_id, item_fingerprint));
ALTER TABLE system.engine_access_fulfillment_outcomes
 DROP CONSTRAINT source_fulfillment_dataitem_binding,
 ADD CONSTRAINT source_fulfillment_dataitem_binding CHECK ((system.valid_source_single_identity(catalog_path, engine_id, binding->>'item_fingerprint')
 AND binding->>'boundary_hash' ~ '^[0-9a-f]{64}$') IS TRUE);

COMMIT;
