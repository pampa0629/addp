UPDATE model.logical_tables
SET materialization = materialization - 'partition_by' - 'partition_type'
WHERE materialization ? 'partition_by'
   OR materialization ? 'partition_type';

ALTER TABLE model.logical_fields
    DROP COLUMN is_partition;
