-- Manager exports now create Transfer ad-hoc sync executions directly.
ALTER TABLE manager.export_sessions
    DROP COLUMN IF EXISTS transfer_task_id;
