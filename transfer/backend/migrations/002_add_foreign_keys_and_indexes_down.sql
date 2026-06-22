-- Migration Rollback: 002_add_transfer_task_indexes

BEGIN;

DROP INDEX IF EXISTS transfer.idx_transfer_tasks_tenant_type;
DROP INDEX IF EXISTS transfer.idx_transfer_tasks_tenant_status;
DROP INDEX IF EXISTS transfer.idx_transfer_tasks_tenant_creator_time;
DROP INDEX IF EXISTS transfer.idx_transfer_tasks_schedule;

COMMIT;
