UPDATE model.logical_tables
SET materialization = COALESCE(materialization, '{}'::jsonb) - 'schema_name' - 'table_name'
WHERE materialization ?| ARRAY['schema_name', 'table_name'];
