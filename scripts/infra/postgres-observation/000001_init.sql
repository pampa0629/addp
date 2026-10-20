-- Deployment-owned PostgreSQL 15 observation boundary, revision 1.
-- Supply observer_scope and observer_verifier as psql variables through private
-- stdin, never command-line secrets. Run with psql -X -q -v ON_ERROR_STOP=1.
\set ON_ERROR_STOP on
\if :{?observer_scope}
\else
  \echo 'Missing observation scope'
  \quit 2
\endif
\if :{?observer_verifier}
\else
  \echo 'Missing observation SCRAM verifier'
  \quit 2
\endif
BEGIN;
SET LOCAL search_path = pg_catalog, pg_temp;
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';
SELECT pg_catalog.set_config('addp.observation_scope', :'observer_scope', true) AS observation_scope,
       pg_catalog.set_config('addp.observation_verifier', :'observer_verifier', true) AS observation_verifier
\gset

DO $install$
DECLARE
    scope text := pg_catalog.current_setting('addp.observation_scope');
    verifier text := pg_catalog.current_setting('addp.observation_verifier');
    reader_name text;
    marker text;
    owner_oid oid;
    reader_oid oid;
    schema_oid oid;
BEGIN
    IF scope NOT IN ('infra', 'business') OR
       verifier !~ '^SCRAM-SHA-256\$4096:[A-Za-z0-9+/]{22}==\$[A-Za-z0-9+/]{43}=:[A-Za-z0-9+/]{43}=$' THEN
        RAISE EXCEPTION 'invalid observation installation input';
    END IF;
    IF pg_catalog.current_setting('server_version_num')::integer NOT BETWEEN 150000 AND 159999 THEN
        RAISE EXCEPTION 'observation revision 1 requires PostgreSQL 15';
    END IF;
    IF NOT (SELECT rolsuper FROM pg_catalog.pg_roles WHERE rolname = CURRENT_USER) THEN
        RAISE EXCEPTION 'observation installation requires deployment administrator';
    END IF;
    PERFORM pg_catalog.pg_advisory_xact_lock(1728330291, 1);
    reader_name := 'addp_' || scope || '_observer';
    marker := 'addp.postgres-observation/v1:' || scope;
    SELECT oid INTO owner_oid FROM pg_catalog.pg_roles WHERE rolname = 'addp_observation_owner';
    SELECT oid INTO reader_oid FROM pg_catalog.pg_roles WHERE rolname = reader_name;

    IF owner_oid IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_roles
        WHERE oid = owner_oid AND NOT rolcanlogin AND rolinherit
          AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole
          AND NOT rolreplication AND NOT rolbypassrls
          AND pg_catalog.shobj_description(oid, 'pg_authid') = marker || ':owner'
    ) THEN
        RAISE EXCEPTION 'observation owner identity conflict';
    END IF;
    IF reader_oid IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_roles
        WHERE oid = reader_oid AND rolcanlogin AND NOT rolinherit
          AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole
          AND NOT rolreplication AND NOT rolbypassrls AND rolconnlimit = 2
          AND pg_catalog.shobj_description(oid, 'pg_authid') = marker || ':reader'
    ) THEN
        RAISE EXCEPTION 'observation login identity conflict';
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_catalog.pg_auth_members m
        WHERE m.member = reader_oid OR m.roleid IN (reader_oid, owner_oid)
           OR (m.member = owner_oid AND (m.roleid <> 'pg_read_all_stats'::regrole OR m.admin_option))
    ) THEN
        RAISE EXCEPTION 'unexpected observation role membership';
    END IF;

    SELECT oid INTO schema_oid FROM pg_catalog.pg_namespace WHERE nspname = 'addp_observation';
    IF schema_oid IS NOT NULL AND (
        pg_catalog.obj_description(schema_oid, 'pg_namespace') IS DISTINCT FROM marker
        OR NOT EXISTS (SELECT 1 FROM pg_catalog.pg_namespace WHERE oid = schema_oid AND nspowner = owner_oid)
        OR EXISTS (SELECT 1 FROM pg_catalog.pg_class WHERE relnamespace = schema_oid)
        OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc WHERE pronamespace = schema_oid
                   AND (proname <> 'snapshot' OR pronargs <> 0 OR proowner <> owner_oid))
        OR EXISTS (
            SELECT 1 FROM pg_catalog.pg_namespace n,
                LATERAL pg_catalog.aclexplode(COALESCE(n.nspacl, pg_catalog.acldefault('n', n.nspowner))) a
            WHERE n.oid = schema_oid AND a.grantee <> owner_oid
              AND (a.grantee <> reader_oid OR a.privilege_type <> 'USAGE' OR a.is_grantable)
        )
        OR EXISTS (
            SELECT 1 FROM pg_catalog.pg_proc p,
                LATERAL pg_catalog.aclexplode(COALESCE(p.proacl, pg_catalog.acldefault('f', p.proowner))) a
            WHERE p.pronamespace = schema_oid AND a.grantee <> owner_oid
              AND (a.grantee <> reader_oid OR a.privilege_type <> 'EXECUTE' OR a.is_grantable)
        )
    ) THEN
        RAISE EXCEPTION 'observation schema identity conflict';
    END IF;
    IF owner_oid IS NULL THEN
        CREATE ROLE addp_observation_owner NOLOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
        EXECUTE pg_catalog.format('COMMENT ON ROLE addp_observation_owner IS %L', marker || ':owner');
    END IF;
    IF reader_oid IS NULL THEN
        EXECUTE pg_catalog.format('CREATE ROLE %I LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 2', reader_name);
        EXECUTE pg_catalog.format('COMMENT ON ROLE %I IS %L', reader_name, marker || ':reader');
    END IF;
    -- NOINHERIT alone does not prevent SET ROLE: the login has no memberships.
    GRANT pg_read_all_stats TO addp_observation_owner;
    EXECUTE pg_catalog.format('ALTER ROLE %I PASSWORD %L', reader_name, verifier);
    EXECUTE pg_catalog.format('ALTER ROLE %I SET default_transaction_read_only = on', reader_name);
    EXECUTE pg_catalog.format('ALTER ROLE %I SET statement_timeout = %L', reader_name, '3s');
    EXECUTE pg_catalog.format('ALTER ROLE %I SET idle_in_transaction_session_timeout = %L', reader_name, '5s');
    EXECUTE pg_catalog.format('ALTER ROLE %I SET search_path = pg_catalog, pg_temp', reader_name);

    -- PUBLIC privileges are additive. Never claim that a per-role REVOKE can
    -- override them, or revoke privileges from unrelated application users.
    IF pg_catalog.has_database_privilege(reader_name, pg_catalog.current_database(), 'CREATE') OR EXISTS (
        SELECT 1 FROM pg_catalog.pg_namespace n
        WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema'
          AND n.nspname <> 'addp_observation'
          AND pg_catalog.has_schema_privilege(reader_name, n.oid, 'CREATE')
    ) OR EXISTS (
        SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema'
          AND CASE WHEN c.relkind = 'S' THEN
              pg_catalog.has_sequence_privilege(reader_name, c.oid, 'USAGE,SELECT,UPDATE') ELSE false END
    ) OR EXISTS (
        SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema'
          AND CASE WHEN c.relkind IN ('r', 'p', 'v', 'm', 'f') THEN
              pg_catalog.has_table_privilege(reader_name, c.oid, 'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')
              OR pg_catalog.has_any_column_privilege(reader_name, c.oid, 'SELECT,INSERT,UPDATE,REFERENCES') ELSE false END
    ) OR EXISTS (
        SELECT 1 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
        WHERE n.nspname !~ '^pg_' AND n.nspname NOT IN ('information_schema', 'addp_observation')
          AND p.prosecdef AND pg_catalog.has_function_privilege(reader_name, p.oid, 'EXECUTE')
    ) THEN
        RAISE EXCEPTION 'observation login has pre-existing application privileges';
    END IF;

    IF schema_oid IS NULL THEN
        CREATE SCHEMA addp_observation AUTHORIZATION addp_observation_owner;
        EXECUTE pg_catalog.format('COMMENT ON SCHEMA addp_observation IS %L', marker);
    END IF;
    REVOKE ALL ON SCHEMA addp_observation FROM PUBLIC;
    EXECUTE pg_catalog.format('GRANT CONNECT ON DATABASE %I TO %I', pg_catalog.current_database(), reader_name);
    EXECUTE pg_catalog.format('GRANT USAGE ON SCHEMA addp_observation TO %I', reader_name);
END
$install$;

CREATE OR REPLACE FUNCTION addp_observation.snapshot()
RETURNS TABLE (
    observed_at timestamptz,
    server_started_at timestamptz,
    statistics_generation text,
    track_counts_enabled boolean,
    connections bigint,
    active_connections bigint,
    idle_connections bigint,
    idle_in_transaction_connections bigint,
    active_waiting_connections bigint,
    active_lock_waiting_connections bigint,
    active_io_waiting_connections bigint,
    max_connections integer,
    transactions_committed bigint,
    transactions_rolled_back bigint,
    deadlocks bigint,
    database_bytes bigint
)
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
SET timezone = 'UTC'
SET stats_fetch_consistency = 'snapshot'
AS $snapshot$
DECLARE
    database_oids oid[];
BEGIN
    SELECT pg_catalog.array_agg(d.oid ORDER BY d.oid) INTO database_oids
    FROM (SELECT oid FROM pg_catalog.pg_database WHERE NOT datistemplate ORDER BY oid LIMIT 257) d;
    IF pg_catalog.cardinality(database_oids) > 256 THEN
        RAISE EXCEPTION 'observation database budget exceeded' USING ERRCODE = '54000';
    END IF;
    RETURN QUERY
    WITH activity AS (
        SELECT pg_catalog.count(*) AS total,
               pg_catalog.count(*) FILTER (WHERE state = 'active') AS active,
               pg_catalog.count(*) FILTER (WHERE state = 'idle') AS idle,
               pg_catalog.count(*) FILTER (WHERE state IN ('idle in transaction', 'idle in transaction (aborted)')) AS idle_transaction,
               pg_catalog.count(*) FILTER (WHERE state = 'active' AND wait_event_type IS NOT NULL) AS waiting,
               pg_catalog.count(*) FILTER (WHERE state = 'active' AND wait_event_type = 'Lock') AS waiting_lock,
               pg_catalog.count(*) FILTER (WHERE state = 'active' AND wait_event_type = 'IO') AS waiting_io
        FROM pg_catalog.pg_stat_activity
        WHERE backend_type = 'client backend' AND pid <> pg_catalog.pg_backend_pid()
    ), statistics AS (
        SELECT pg_catalog.md5(pg_catalog.string_agg(d.datid::text || ':' || COALESCE(d.stats_reset::text, '-'), ',' ORDER BY d.datid)) AS generation,
               pg_catalog.sum(d.xact_commit)::bigint AS committed,
               pg_catalog.sum(d.xact_rollback)::bigint AS rolled_back,
               pg_catalog.sum(d.deadlocks)::bigint AS deadlocked
        FROM pg_catalog.pg_stat_database d WHERE d.datid = ANY(database_oids)
    ), sizes AS (
        SELECT CASE WHEN pg_catalog.count(bytes) = pg_catalog.cardinality(database_oids)
                    THEN pg_catalog.sum(bytes)::bigint END AS total_bytes
        FROM (SELECT pg_catalog.pg_database_size(id) AS bytes FROM pg_catalog.unnest(database_oids) id) d
    )
    SELECT pg_catalog.statement_timestamp(), pg_catalog.pg_postmaster_start_time(), s.generation,
           pg_catalog.current_setting('track_counts')::boolean,
           a.total, a.active, a.idle, a.idle_transaction, a.waiting, a.waiting_lock, a.waiting_io,
           pg_catalog.current_setting('max_connections')::integer,
           CASE WHEN pg_catalog.current_setting('track_counts')::boolean THEN s.committed END,
           CASE WHEN pg_catalog.current_setting('track_counts')::boolean THEN s.rolled_back END,
           CASE WHEN pg_catalog.current_setting('track_counts')::boolean THEN s.deadlocked END,
           d.total_bytes
    FROM activity a CROSS JOIN statistics s CROSS JOIN sizes d;
END
$snapshot$;
ALTER FUNCTION addp_observation.snapshot() OWNER TO addp_observation_owner;
REVOKE ALL ON FUNCTION addp_observation.snapshot() FROM PUBLIC;
DO $grant$
DECLARE
    reader_oid oid;
    owner_oid oid := 'addp_observation_owner'::regrole;
BEGIN
    EXECUTE pg_catalog.format('GRANT EXECUTE ON FUNCTION addp_observation.snapshot() TO %I',
        'addp_' || pg_catalog.current_setting('addp.observation_scope') || '_observer');
    SELECT oid INTO reader_oid FROM pg_catalog.pg_roles
    WHERE rolname = 'addp_' || pg_catalog.current_setting('addp.observation_scope') || '_observer';
    -- Deployment administrator defaults may grant privileges to unrelated
    -- roles. Do not publish a new privileged function under those defaults.
    IF EXISTS (
        SELECT 1 FROM pg_catalog.pg_proc p, LATERAL pg_catalog.aclexplode(p.proacl) a
        WHERE p.oid = 'addp_observation.snapshot()'::regprocedure
          AND a.grantee <> owner_oid
          AND (a.grantee <> reader_oid OR a.privilege_type <> 'EXECUTE' OR a.is_grantable)
    ) OR EXISTS (
        SELECT 1 FROM pg_catalog.pg_namespace n, LATERAL pg_catalog.aclexplode(n.nspacl) a
        WHERE n.nspname = 'addp_observation' AND a.grantee <> owner_oid
          AND (a.grantee <> reader_oid OR a.privilege_type <> 'USAGE' OR a.is_grantable)
    ) THEN
        RAISE EXCEPTION 'unexpected observation object default privileges';
    END IF;
END
$grant$;
COMMIT;
