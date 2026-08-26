DROP INDEX model.uq_model_materialization_active_target;

CREATE UNIQUE INDEX uq_model_materialization_active_target
    ON model.materialization_batches(tenant_id, engine_id, target_parent_locator, target_name)
    WHERE status IN ('preparing', 'prepared', 'publishing');
