BEGIN;

ALTER TABLE system.module_definitions
    ADD COLUMN task_provider jsonb;

DO $$
BEGIN
    IF to_regclass('system.task_providers') IS NOT NULL THEN
        IF EXISTS (
            SELECT 1
            FROM system.task_providers AS provider
            LEFT JOIN system.module_definitions AS module
              ON module.module_name = provider.module_name
            WHERE module.id IS NULL
        ) THEN
            RAISE EXCEPTION 'every TaskProvider must belong to an existing module definition';
        END IF;

        UPDATE system.module_definitions AS module
        SET task_provider = jsonb_strip_nulls(jsonb_build_object(
                'display_name', provider.display_name,
                'description', provider.description,
                'task_list_endpoint', provider.task_list_endpoint,
                'task_detail_endpoint', provider.task_detail_endpoint,
                'task_execute_endpoint', provider.task_execute_endpoint,
                'task_status_endpoint', provider.task_status_endpoint,
                'task_cancel_endpoint', nullif(provider.task_cancel_endpoint, ''),
                'capabilities', provider.capabilities
            )),
            version = module.version + 1,
            updated_at = now()
        FROM system.task_providers AS provider
        WHERE module.module_name = provider.module_name;

        DROP TABLE system.task_providers;
    END IF;
END $$;

COMMIT;
