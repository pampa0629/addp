CREATE SCHEMA IF NOT EXISTS addp_business;
CREATE TABLE IF NOT EXISTS addp_business.addp_engine_probe (
    id integer PRIMARY KEY,
    engine_name text NOT NULL
);
INSERT INTO addp_business.addp_engine_probe (id, engine_name)
VALUES (1, 'HighGo V4.5 Business')
ON CONFLICT (id) DO UPDATE SET engine_name = EXCLUDED.engine_name;
