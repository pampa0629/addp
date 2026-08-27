UPDATE model.logical_tables
SET materialization = materialization - 'partition_by' - 'partition_type'
WHERE (materialization ? 'partition_by' OR materialization ? 'partition_type')
  AND (
    NOT materialization ? 'partition_by'
    OR materialization->'partition_by' = 'null'::jsonb
    OR (
      jsonb_typeof(materialization->'partition_by') = 'string'
      AND btrim(materialization->>'partition_by') = ''
    )
  );
