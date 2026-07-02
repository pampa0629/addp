-- 034_rename_preview_artifact_tables.sql
-- Clean old Manager preview artifact tables and keep only the unified terminology.

DELETE FROM common.task_executions
WHERE module = 'manager'
  AND task_type IN (
      'quick_view_optimization',
      'vector_quick_view_target_generation',
      'tile_cache_generation',
      'cog_artifact_generation',
      'mvt_generation',
      'model_3d_quick_view_generation',
      'gaussian_splat_quick_view_generation'
  );

DROP INDEX IF EXISTS manager.idx_qvo_tasks_tenant;
DROP INDEX IF EXISTS manager.idx_qvo_tasks_schedule;
DROP INDEX IF EXISTS manager.idx_qvo_tasks_last_execution;
DROP INDEX IF EXISTS manager.idx_qvo_tasks_deleted_at;
DROP INDEX IF EXISTS manager.idx_qvo_tenant_item_fingerprint;
DROP INDEX IF EXISTS manager.idx_qvo_tenant_item;
DROP INDEX IF EXISTS manager.idx_qvo_task;
DROP INDEX IF EXISTS manager.idx_qvo_execution;
DROP INDEX IF EXISTS manager.idx_qvo_status;
DROP INDEX IF EXISTS manager.idx_qvo_deleted_at;
DROP INDEX IF EXISTS manager.idx_qvo_current_target_unique;
DROP INDEX IF EXISTS manager.idx_model_3d_quick_view_tasks_tenant;
DROP INDEX IF EXISTS manager.idx_model_3d_quick_view_tasks_last_execution;
DROP INDEX IF EXISTS manager.idx_model_3d_quick_view_tasks_deleted_at;
DROP INDEX IF EXISTS manager.idx_model_3d_quick_view_tasks_source_unique;
DROP INDEX IF EXISTS manager.idx_model_3d_quick_view_tenant_item;
DROP INDEX IF EXISTS manager.idx_model_3d_quick_view_tenant_item_fingerprint;
DROP INDEX IF EXISTS manager.idx_model_3d_quick_view_task;
DROP INDEX IF EXISTS manager.idx_model_3d_quick_view_execution;
DROP INDEX IF EXISTS manager.idx_model_3d_quick_view_status;
DROP INDEX IF EXISTS manager.idx_model_3d_quick_view_deleted_at;
DROP INDEX IF EXISTS manager.idx_model_3d_quick_view_current_unique;
DROP INDEX IF EXISTS manager.idx_gaussian_splat_quick_view_tasks_tenant;
DROP INDEX IF EXISTS manager.idx_gaussian_splat_quick_view_tasks_last_execution;
DROP INDEX IF EXISTS manager.idx_gaussian_splat_quick_view_tasks_deleted_at;
DROP INDEX IF EXISTS manager.idx_gaussian_splat_quick_view_tasks_source_unique;
DROP INDEX IF EXISTS manager.idx_gaussian_splat_quick_view_tenant_item;
DROP INDEX IF EXISTS manager.idx_gaussian_splat_quick_view_tenant_item_fingerprint;
DROP INDEX IF EXISTS manager.idx_gaussian_splat_quick_view_task;
DROP INDEX IF EXISTS manager.idx_gaussian_splat_quick_view_execution;
DROP INDEX IF EXISTS manager.idx_gaussian_splat_quick_view_status;
DROP INDEX IF EXISTS manager.idx_gaussian_splat_quick_view_deleted_at;
DROP INDEX IF EXISTS manager.idx_gaussian_splat_quick_view_current_unique;

DROP TABLE IF EXISTS manager.vector_quick_view_target_tasks;
DROP TABLE IF EXISTS manager.vector_quick_view_targets;
DROP TABLE IF EXISTS manager.model_3d_quick_view_tasks;
DROP TABLE IF EXISTS manager.model_3d_quick_view;
DROP TABLE IF EXISTS manager.gaussian_splat_quick_view_tasks;
DROP TABLE IF EXISTS manager.gaussian_splat_quick_view;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('manager.preview_state')
          AND conname = 'quick_view_pkey'
    ) AND NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('manager.preview_state')
          AND conname = 'preview_state_pkey'
    ) THEN
        ALTER TABLE manager.preview_state RENAME CONSTRAINT quick_view_pkey TO preview_state_pkey;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('manager.vector_materialized_view_tasks')
          AND conname = 'vector_quick_view_target_tasks_pkey'
    ) AND NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('manager.vector_materialized_view_tasks')
          AND conname = 'vector_materialized_view_tasks_pkey'
    ) THEN
        ALTER TABLE manager.vector_materialized_view_tasks RENAME CONSTRAINT vector_quick_view_target_tasks_pkey TO vector_materialized_view_tasks_pkey;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('manager.vector_materialized_view')
          AND conname = 'vector_quick_view_targets_pkey'
    ) AND NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('manager.vector_materialized_view')
          AND conname = 'vector_materialized_view_pkey'
    ) THEN
        ALTER TABLE manager.vector_materialized_view RENAME CONSTRAINT vector_quick_view_targets_pkey TO vector_materialized_view_pkey;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('manager.model_3d_glb_tasks')
          AND conname = 'model_3d_quick_view_tasks_pkey'
    ) AND NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('manager.model_3d_glb_tasks')
          AND conname = 'model_3d_glb_tasks_pkey'
    ) THEN
        ALTER TABLE manager.model_3d_glb_tasks RENAME CONSTRAINT model_3d_quick_view_tasks_pkey TO model_3d_glb_tasks_pkey;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('manager.model_3d_glb')
          AND conname = 'model_3d_quick_view_pkey'
    ) AND NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('manager.model_3d_glb')
          AND conname = 'model_3d_glb_pkey'
    ) THEN
        ALTER TABLE manager.model_3d_glb RENAME CONSTRAINT model_3d_quick_view_pkey TO model_3d_glb_pkey;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('manager.gaussian_splat_ksplat_tasks')
          AND conname = 'gaussian_splat_quick_view_tasks_pkey'
    ) AND NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('manager.gaussian_splat_ksplat_tasks')
          AND conname = 'gaussian_splat_ksplat_tasks_pkey'
    ) THEN
        ALTER TABLE manager.gaussian_splat_ksplat_tasks RENAME CONSTRAINT gaussian_splat_quick_view_tasks_pkey TO gaussian_splat_ksplat_tasks_pkey;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('manager.gaussian_splat_ksplat')
          AND conname = 'gaussian_splat_quick_view_pkey'
    ) AND NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('manager.gaussian_splat_ksplat')
          AND conname = 'gaussian_splat_ksplat_pkey'
    ) THEN
        ALTER TABLE manager.gaussian_splat_ksplat RENAME CONSTRAINT gaussian_splat_quick_view_pkey TO gaussian_splat_ksplat_pkey;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_tasks_tenant
    ON manager.vector_materialized_view_tasks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_tasks_schedule
    ON manager.vector_materialized_view_tasks (enabled, next_run_at);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_tasks_last_execution
    ON manager.vector_materialized_view_tasks (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_tasks_deleted_at
    ON manager.vector_materialized_view_tasks (deleted_at);

CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_tenant_item_fingerprint
    ON manager.vector_materialized_view (tenant_id, item_fingerprint);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_tenant_item
    ON manager.vector_materialized_view (tenant_id, item_id);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_task
    ON manager.vector_materialized_view (task_id);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_execution
    ON manager.vector_materialized_view (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_status
    ON manager.vector_materialized_view (status);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_deleted_at
    ON manager.vector_materialized_view (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_vector_materialized_view_current_unique
    ON manager.vector_materialized_view (tenant_id, item_fingerprint, source_geometry_column, target_srid)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_model_3d_glb_tasks_tenant
    ON manager.model_3d_glb_tasks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_model_3d_glb_tasks_last_execution
    ON manager.model_3d_glb_tasks (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_model_3d_glb_tasks_deleted_at
    ON manager.model_3d_glb_tasks (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_model_3d_glb_tasks_source_unique
    ON manager.model_3d_glb_tasks (tenant_id, ((config->'source'->>'item_fingerprint')))
    WHERE deleted_at IS NULL AND COALESCE(config->'source'->>'item_fingerprint', '') <> '';

CREATE INDEX IF NOT EXISTS idx_model_3d_glb_tenant_item
    ON manager.model_3d_glb (tenant_id, item_id);
CREATE INDEX IF NOT EXISTS idx_model_3d_glb_tenant_item_fingerprint
    ON manager.model_3d_glb (tenant_id, item_fingerprint);
CREATE INDEX IF NOT EXISTS idx_model_3d_glb_task
    ON manager.model_3d_glb (task_id);
CREATE INDEX IF NOT EXISTS idx_model_3d_glb_execution
    ON manager.model_3d_glb (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_model_3d_glb_status
    ON manager.model_3d_glb (status);
CREATE INDEX IF NOT EXISTS idx_model_3d_glb_deleted_at
    ON manager.model_3d_glb (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_model_3d_glb_current_unique
    ON manager.model_3d_glb (tenant_id, item_fingerprint)
    WHERE deleted_at IS NULL AND status <> 'deleted';

CREATE INDEX IF NOT EXISTS idx_gaussian_splat_ksplat_tasks_tenant
    ON manager.gaussian_splat_ksplat_tasks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_gaussian_splat_ksplat_tasks_last_execution
    ON manager.gaussian_splat_ksplat_tasks (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_gaussian_splat_ksplat_tasks_deleted_at
    ON manager.gaussian_splat_ksplat_tasks (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_gaussian_splat_ksplat_tasks_source_unique
    ON manager.gaussian_splat_ksplat_tasks (tenant_id, ((config->'source'->>'item_fingerprint')))
    WHERE deleted_at IS NULL AND COALESCE(config->'source'->>'item_fingerprint', '') <> '';

CREATE INDEX IF NOT EXISTS idx_gaussian_splat_ksplat_tenant_item
    ON manager.gaussian_splat_ksplat (tenant_id, item_id);
CREATE INDEX IF NOT EXISTS idx_gaussian_splat_ksplat_tenant_item_fingerprint
    ON manager.gaussian_splat_ksplat (tenant_id, item_fingerprint);
CREATE INDEX IF NOT EXISTS idx_gaussian_splat_ksplat_task
    ON manager.gaussian_splat_ksplat (task_id);
CREATE INDEX IF NOT EXISTS idx_gaussian_splat_ksplat_execution
    ON manager.gaussian_splat_ksplat (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_gaussian_splat_ksplat_status
    ON manager.gaussian_splat_ksplat (status);
CREATE INDEX IF NOT EXISTS idx_gaussian_splat_ksplat_deleted_at
    ON manager.gaussian_splat_ksplat (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_gaussian_splat_ksplat_current_unique
    ON manager.gaussian_splat_ksplat (tenant_id, item_fingerprint)
    WHERE deleted_at IS NULL AND status <> 'deleted';

COMMENT ON TABLE manager.vector_materialized_view_tasks IS '矢量物化视图任务定义表';
COMMENT ON TABLE manager.vector_materialized_view IS '矢量物化视图结果状态表';
COMMENT ON TABLE manager.model_3d_glb_tasks IS '三维模型 GLB 生成任务定义表';
COMMENT ON TABLE manager.model_3d_glb IS 'Manager 受管三维模型 GLB 结果表';
COMMENT ON TABLE manager.gaussian_splat_ksplat_tasks IS '3DGS KSplat 生成任务定义表';
COMMENT ON TABLE manager.gaussian_splat_ksplat IS 'Manager 受管 3DGS KSplat 结果表';
