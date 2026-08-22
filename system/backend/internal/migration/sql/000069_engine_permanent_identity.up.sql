BEGIN;

ALTER TABLE system.engines
    ADD COLUMN identity_key jsonb,
    ADD COLUMN version bigint NOT NULL DEFAULT 1,
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN deleted_by bigint,
    ADD COLUMN restored_at timestamptz,
    ADD COLUMN restored_by bigint;

-- 既有记录按当前插件身份规则回填。localhost 别名必须使用持久规范值，
-- 不能受 RESOURCE_LOCALHOST_ALIAS 等运行环境变量影响。
UPDATE system.engines
SET identity_key = CASE lower(btrim(engine_type))
    WHEN 'postgresql' THEN jsonb_build_object(
        'database', coalesce(connection_info->>'database', ''),
        'host', CASE lower(btrim(coalesce(connection_info->>'host', '')))
            WHEN 'localhost' THEN '127.0.0.1'
            WHEN 'host.docker.internal' THEN '127.0.0.1'
            ELSE lower(btrim(coalesce(connection_info->>'host', '')))
        END,
        'port', coalesce(nullif(btrim(connection_info->>'port'), ''), '5432')
    )
    WHEN 'mysql' THEN jsonb_build_object(
        'database', coalesce(connection_info->>'database', ''),
        'host', CASE lower(btrim(coalesce(connection_info->>'host', '')))
            WHEN 'localhost' THEN '127.0.0.1'
            WHEN 'host.docker.internal' THEN '127.0.0.1'
            ELSE lower(btrim(coalesce(connection_info->>'host', '')))
        END,
        'port', coalesce(nullif(btrim(connection_info->>'port'), ''), '3306')
    )
    WHEN 'mongodb' THEN jsonb_build_object(
        'auth_source', coalesce(nullif(btrim(connection_info->>'auth_source'), ''), 'admin'),
        'host', CASE lower(btrim(coalesce(connection_info->>'host', '')))
            WHEN 'localhost' THEN '127.0.0.1'
            WHEN 'host.docker.internal' THEN '127.0.0.1'
            ELSE lower(btrim(coalesce(connection_info->>'host', '')))
        END,
        'port', coalesce(nullif(btrim(connection_info->>'port'), ''), '27017'),
        'user', btrim(coalesce(connection_info->>'user', ''))
    )
    WHEN 'minio' THEN jsonb_build_object(
        'endpoint', lower(rtrim(btrim(coalesce(connection_info->>'endpoint', '')), '/'))
    )
    WHEN 's3' THEN jsonb_build_object(
        'endpoint', lower(rtrim(btrim(coalesce(connection_info->>'endpoint', '')), '/'))
    )
    WHEN 'nfs' THEN jsonb_build_object(
        'export_path', CASE btrim(coalesce(connection_info->>'export_path', ''))
            WHEN '/' THEN '/'
            ELSE rtrim(btrim(coalesce(connection_info->>'export_path', '')), '/')
        END,
        'server', CASE lower(btrim(coalesce(connection_info->>'server', '')))
            WHEN 'localhost' THEN '127.0.0.1'
            WHEN 'host.docker.internal' THEN '127.0.0.1'
            ELSE lower(btrim(coalesce(connection_info->>'server', '')))
        END
    )
    WHEN 'doris' THEN jsonb_build_object(
        'database', coalesce(connection_info->>'database', ''),
        'host', CASE lower(btrim(coalesce(connection_info->>'host', '')))
            WHEN 'localhost' THEN '127.0.0.1'
            WHEN 'host.docker.internal' THEN '127.0.0.1'
            ELSE lower(btrim(coalesce(connection_info->>'host', '')))
        END,
        'port', coalesce(nullif(btrim(connection_info->>'port'), ''), '9030')
    )
    WHEN 'clickhouse' THEN jsonb_build_object(
        'database', coalesce(connection_info->>'database', ''),
        'host', CASE lower(btrim(coalesce(connection_info->>'host', '')))
            WHEN 'localhost' THEN '127.0.0.1'
            WHEN 'host.docker.internal' THEN '127.0.0.1'
            ELSE lower(btrim(coalesce(connection_info->>'host', '')))
        END,
        'port', coalesce(nullif(btrim(connection_info->>'port'), ''), '9000')
    )
    WHEN 'spark_sql' THEN jsonb_build_object(
        'database', coalesce(connection_info->>'database', ''),
        'host', CASE lower(btrim(coalesce(connection_info->>'host', '')))
            WHEN 'localhost' THEN '127.0.0.1'
            WHEN 'host.docker.internal' THEN '127.0.0.1'
            ELSE lower(btrim(coalesce(connection_info->>'host', '')))
        END,
        'port', coalesce(nullif(btrim(connection_info->>'port'), ''), '10000')
    )
    WHEN 'neo4j' THEN jsonb_build_object(
        'host', CASE lower(btrim(coalesce(connection_info->>'host', '')))
            WHEN 'localhost' THEN '127.0.0.1'
            WHEN 'host.docker.internal' THEN '127.0.0.1'
            ELSE lower(btrim(coalesce(connection_info->>'host', '')))
        END,
        'port', coalesce(nullif(btrim(connection_info->>'port'), ''), '7687')
    )
    WHEN 'kafka' THEN jsonb_build_object(
        'bootstrap_servers', btrim(coalesce(connection_info->>'bootstrap_servers', ''))
    )
    WHEN 'oracle' THEN jsonb_build_object(
        'host', CASE lower(btrim(coalesce(connection_info->>'host', '')))
            WHEN 'localhost' THEN '127.0.0.1'
            WHEN 'host.docker.internal' THEN '127.0.0.1'
            ELSE lower(btrim(coalesce(connection_info->>'host', '')))
        END,
        'port', coalesce(nullif(btrim(connection_info->>'port'), ''), '1521'),
        'service_name', btrim(coalesce(connection_info->>'service_name', '')),
        'user', btrim(coalesce(connection_info->>'user', ''))
    )
    ELSE jsonb_build_object(
        'host', CASE lower(btrim(coalesce(connection_info->>'host', '')))
            WHEN 'localhost' THEN '127.0.0.1'
            WHEN 'host.docker.internal' THEN '127.0.0.1'
            ELSE lower(btrim(coalesce(connection_info->>'host', '')))
        END,
        'port', coalesce(nullif(btrim(connection_info->>'port'), ''), '0'),
        'protocol', coalesce(nullif(lower(btrim(connection_info->>'protocol')), ''), 'http')
    )
END;

-- 历史版本可能已经为同一物理身份创建过多条记录。最早的 ID 作为永久
-- 身份继续使用；其余 ID 不能物理删除，也不能继续参与选择，因此隔离成
-- 带迁移标识的 deleted 墓碑。后续同一身份注册仍会命中最早的 ID。
WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY tenant_id, lower(engine_type), identity_key
               ORDER BY id
           ) AS identity_rank
    FROM system.engines
)
UPDATE system.engines AS engine
SET identity_key = engine.identity_key || jsonb_build_object('_legacy_duplicate_id', engine.id),
    lifecycle_state = 'deleted',
    connection_info = '{}'::jsonb,
    connection_status = 'unknown',
    last_check_at = NULL,
    check_message = '',
    deleted_at = coalesce(engine.deleted_at, now()),
    version = engine.version + 1,
    updated_at = now()
FROM ranked
WHERE ranked.id = engine.id
  AND ranked.identity_rank > 1;

-- SQLite/SpatiaLite 属于文件容器而不是 System Engine Instance；旧的内置
-- math_workflow 示例也已退出引擎体系。历史记录只做墓碑化，永久 ID 保留。
UPDATE system.engines
SET lifecycle_state = 'deleted',
    connection_info = '{}'::jsonb,
    connection_status = 'unknown',
    last_check_at = NULL,
    check_message = '',
    deleted_at = coalesce(deleted_at, now()),
    version = version + 1,
    updated_at = now()
WHERE lifecycle_state <> 'deleted'
  AND (
      lower(engine_type) IN ('sqlite', 'spatialite')
      OR (lower(engine_type) = 'math_workflow' AND is_builtin = true)
  );

ALTER TABLE system.engines
    ALTER COLUMN identity_key SET NOT NULL,
    ADD CONSTRAINT ck_engines_version_positive CHECK (version > 0),
    ADD CONSTRAINT ck_engines_lifecycle_state CHECK (lifecycle_state IN ('active', 'disabled', 'deleting', 'deleted'));

CREATE UNIQUE INDEX uq_engines_tenant_type_identity
    ON system.engines (tenant_id, lower(engine_type), identity_key)
    WHERE tenant_id IS NOT NULL;

CREATE UNIQUE INDEX uq_engines_platform_type_identity
    ON system.engines (lower(engine_type), identity_key)
    WHERE tenant_id IS NULL;

COMMIT;
