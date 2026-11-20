CREATE TABLE system.module_discovery_snapshots (
    module_name text PRIMARY KEY,
    binding_id text NOT NULL,
    endpoints jsonb NOT NULL CHECK (jsonb_typeof(endpoints) = 'array')
);
