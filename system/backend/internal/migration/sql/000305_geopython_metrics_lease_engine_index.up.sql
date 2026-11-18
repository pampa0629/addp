BEGIN;

CREATE INDEX idx_geopython_metrics_lease_engine_id
    ON system.geopython_metrics_lease (engine_id);

COMMIT;
