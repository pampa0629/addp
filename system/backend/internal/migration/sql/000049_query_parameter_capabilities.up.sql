UPDATE system.engines
SET capabilities = jsonb_set(
    capabilities,
    '{compute,query,parameters}',
    CASE engine_type
        WHEN 'mongodb' THEN '{"supported":true,"languages":["mql"],"types":["string","integer","number","boolean"]}'::jsonb
        WHEN 'neo4j' THEN '{"supported":true,"languages":["cypher"],"types":["string","integer","number","boolean"]}'::jsonb
        ELSE '{"supported":true,"languages":["sql"],"types":["string","integer","number","boolean"]}'::jsonb
    END,
    true
)
WHERE engine_type IN ('postgresql', 'mysql', 'doris', 'clickhouse', 'mongodb', 'neo4j')
  AND capabilities IS NOT NULL
  AND capabilities->'compute'->'query'->>'supported' = 'true';
