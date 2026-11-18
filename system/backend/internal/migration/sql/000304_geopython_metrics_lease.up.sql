BEGIN;

CREATE TABLE system.geopython_metrics_lease (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    engine_id bigint NOT NULL REFERENCES system.engines (id) ON DELETE CASCADE,
    instance_id uuid NOT NULL,
    started_at timestamptz NOT NULL,
    endpoint varchar(512) NOT NULL,
    lease_expires_at timestamptz NOT NULL CHECK (lease_expires_at > started_at)
);

COMMIT;
