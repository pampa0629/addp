BEGIN;

CREATE TABLE system.module_registry_state (
    id bigint PRIMARY KEY CHECK (id = 1),
    revision bigint NOT NULL CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO system.module_registry_state (id, revision) VALUES (1, 1);

COMMIT;
