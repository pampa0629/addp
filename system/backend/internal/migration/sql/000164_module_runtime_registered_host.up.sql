BEGIN;

ALTER TABLE system.module_runtime_instances
    ADD COLUMN registered_host varchar(255) NOT NULL DEFAULT '';

UPDATE system.module_runtime_instances
SET registered_host = coalesce(lower(btrim(
    substring(coalesce(nullif(module_url, ''), nullif(health_check_url, ''))
        from '^https?://(\[[^]]+\]|[^:/?#]+)'),
    '[]'
)), '')
WHERE coalesce(nullif(module_url, ''), nullif(health_check_url, '')) IS NOT NULL;

CREATE INDEX idx_module_runtime_instances_registered_host_registered
    ON system.module_runtime_instances (registered_host, registered_at DESC, id DESC);

CREATE INDEX idx_module_runtime_instances_registered
    ON system.module_runtime_instances (registered_at DESC, id DESC);

COMMIT;
