ALTER TABLE service.query_services
    ADD COLUMN IF NOT EXISTS runtime_engine_id bigint REFERENCES system.engines(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_query_services_runtime_engine
    ON service.query_services(runtime_engine_id);

DELETE FROM service.query_services
WHERE config_type = 'sql' AND engine_id IS NULL;

ALTER TABLE service.query_services
    DROP CONSTRAINT IF EXISTS query_services_explicit_execution_engine_check;

ALTER TABLE service.query_services
    ADD CONSTRAINT query_services_explicit_execution_engine_check CHECK (
        (config_type = 'sql' AND ((engine_id IS NOT NULL) <> (runtime_engine_id IS NOT NULL)))
        OR
        (config_type = 'table' AND engine_id IS NOT NULL)
    );

COMMENT ON COLUMN service.query_services.engine_id IS
'源存储引擎 ID；联邦 SQL 的来源由 SQL 和发布快照表达';
COMMENT ON COLUMN service.query_services.runtime_engine_id IS
'显式联邦查询 Runtime Engine ID；不得通过 engine_id 空值推断';
