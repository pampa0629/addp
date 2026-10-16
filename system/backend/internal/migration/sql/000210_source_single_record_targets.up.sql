BEGIN;

-- Extend the one identity predicate used by Grant, approval, Deny and receipt
-- constraints. Existing schema/table identities and authority remain unchanged.
CREATE OR REPLACE FUNCTION system.valid_source_single_identity(path jsonb, engine bigint, fingerprint text)
RETURNS boolean LANGUAGE sql IMMUTABLE STRICT SET search_path = pg_catalog AS $$
 SELECT (jsonb_typeof(path) = 'object' AND path->>'version' = 'catalog.path/v1'
 AND path->>'engine_id' = engine::text AND engine > 0
 AND jsonb_typeof(path->'segments') = 'array' AND jsonb_array_length(path->'segments') = 3
 AND path->'segments'->0->>'term' = 'server' AND path->'segments'->0->>'kind' = 'server'
 AND path->'segments'->0->>'name' = ''
 AND path->'segments'->1->>'kind' = 'namespace'
 AND ((path->'segments'->1->>'term' IN ('schema', 'database')
       AND path->'segments'->2->>'term' = 'table' AND path->'segments'->2->>'kind' = 'table')
   OR (path->'segments'->1->>'term' = 'database'
       AND path->'segments'->2->>'term' = 'collection' AND path->'segments'->2->>'kind' = 'collection'))
 AND length(path->'segments'->1->>'name') > 0 AND length(path->'segments'->2->>'name') > 0
 AND fingerprint ~ '^[0-9a-f]{64}$'
 AND fingerprint = encode(sha256(convert_to(engine::text || ':' ||
   (path->'segments'->1->>'name') || '.' || (path->'segments'->2->>'name'), 'UTF8')), 'hex')) IS TRUE
$$;

COMMIT;
