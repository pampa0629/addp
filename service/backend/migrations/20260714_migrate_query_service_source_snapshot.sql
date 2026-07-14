-- 查询服务依赖快照单一路径迁移。
-- 运行时只读取 data_config.source_snapshot；旧 geometry/object_table 节点在本迁移中删除。
-- 无法可靠形成新快照的旧记录直接删除：
--   1. 表模式无法通过 locator 定位同租户且具有 fingerprint 的 Meta item；
--   2. engine_id 为空的旧 SQL 模式（DuckDB 联邦对象表依赖无法从旧配置还原）。

DELETE FROM service.query_services qs
WHERE qs.config_type = 'table'
  AND NOT (qs.data_config ? 'source_snapshot')
  AND NOT EXISTS (
      SELECT 1
      FROM meta.meta_item mi
      WHERE mi.id = NULLIF(
          substring(qs.data_config ->> 'locator' from '[?&]item_id=([0-9]+)'),
          ''
      )::bigint
        AND mi.tenant_id = qs.tenant_id
        AND NULLIF(btrim(mi.fingerprint), '') IS NOT NULL
  );

DELETE FROM service.query_services qs
WHERE qs.config_type = 'sql'
  AND qs.engine_id IS NULL
  AND NOT (qs.data_config ? 'source_snapshot');

WITH table_sources AS (
    SELECT
        qs.id AS service_id,
        qs.updated_at,
        mi.id AS item_id,
        mi.fingerprint,
        mi.scanned_at,
        mi.data_updated_at,
        mi.item_type,
        mi.name AS item_name,
        mi.attributes
    FROM service.query_services qs
    JOIN meta.meta_item mi
      ON mi.id = NULLIF(
          substring(qs.data_config ->> 'locator' from '[?&]item_id=([0-9]+)'),
          ''
      )::bigint
     AND mi.tenant_id = qs.tenant_id
     AND NULLIF(btrim(mi.fingerprint), '') IS NOT NULL
    WHERE qs.config_type = 'table'
      AND NOT (qs.data_config ? 'source_snapshot')
), table_snapshots AS (
    SELECT
        service_id,
        jsonb_strip_nulls(jsonb_build_object(
            'source', CASE WHEN item_id IS NOT NULL THEN jsonb_strip_nulls(jsonb_build_object(
                'item_id', item_id,
                'item_fingerprint', fingerprint,
                'scanned_at', scanned_at,
                'data_updated_at', data_updated_at
            )) END,
            'captured_at', updated_at,
            'verification_status', 'unverifiable',
            'table', CASE
                WHEN jsonb_typeof(attributes #> '{type_info,table}') = 'object'
                THEN jsonb_strip_nulls(jsonb_build_object(
                    'name', COALESCE(attributes #>> '{type_info,table,name}', item_name),
                    'kind', attributes #>> '{type_info,table,kind}',
                    'fields', attributes #> '{type_info,table,fields}',
                    'primary_key', attributes #> '{type_info,table,primary_key}'
                ))
            END,
            'spatial', CASE
                WHEN jsonb_typeof(attributes #> '{capabilities,spatial}') = 'object'
                THEN attributes #> '{capabilities,spatial}'
            END,
            'object_table', CASE
                WHEN item_type IN ('object', 'file')
                 AND lower(trim(leading '.' from COALESCE(attributes #>> '{item,format}', ''))) IN ('parquet', 'orc', 'avro')
                THEN jsonb_strip_nulls(jsonb_build_object(
                    'layout', COALESCE(NULLIF(attributes #>> '{item,layout}', ''), 'single'),
                    'data_type', attributes #>> '{item,data_type}',
                    'format', lower(trim(leading '.' from attributes #>> '{item,format}')),
                    'primary_content_path', CASE
                        WHEN COALESCE(NULLIF(attributes #>> '{item,layout}', ''), 'single') IN ('single', 'multi')
                        THEN attributes #>> '{storage,physical_path}'
                    END,
                    'scope_path', CASE
                        WHEN attributes #>> '{item,layout}' = 'whole'
                        THEN COALESCE(attributes #>> '{storage,physical_path}', attributes #>> '{storage,path}')
                    END,
                    'physical_path', attributes #>> '{storage,physical_path}',
                    'storage_path', attributes #>> '{storage,path}',
                    'storage_name', attributes #>> '{storage,name}',
                    'storage_bucket', attributes #>> '{storage,bucket}',
                    'refs', attributes #> '{item,refs}'
                ))
            END
        )) AS snapshot
    FROM table_sources
)
UPDATE service.query_services qs
SET data_config = (qs.data_config - 'geometry' - 'object_table')
    || jsonb_build_object('source_snapshot', table_snapshots.snapshot)
FROM table_snapshots
WHERE qs.id = table_snapshots.service_id;

WITH sql_sources AS (
    SELECT
        id,
        updated_at,
        data_config -> 'geometry' AS geometry
    FROM service.query_services
    WHERE config_type = 'sql'
      AND engine_id IS NOT NULL
      AND NOT (data_config ? 'source_snapshot')
), sql_snapshots AS (
    SELECT
        id,
        jsonb_strip_nulls(jsonb_build_object(
            'captured_at', updated_at,
            'verification_status', 'unverifiable',
            'spatial', CASE
                WHEN geometry ->> 'has_geometry' = 'true'
                 AND COALESCE(geometry ->> 'column', '') <> ''
                THEN jsonb_strip_nulls(jsonb_build_object(
                    'geometry_columns', jsonb_build_array(jsonb_strip_nulls(jsonb_build_object(
                        'name', geometry ->> 'column',
                        'geometry_type', COALESCE(geometry -> 'types' ->> 0, 'Geometry'),
                        'srid', CASE WHEN COALESCE(geometry ->> 'srid', '0')::integer > 0 THEN (geometry ->> 'srid')::integer END,
                        'crs_ref', CASE WHEN COALESCE(geometry ->> 'srid', '0')::integer > 0 THEN 'EPSG:' || (geometry ->> 'srid') END
                    ))),
                    'primary_geometry_column', geometry ->> 'column',
                    'extent', CASE
                        WHEN jsonb_typeof(geometry -> 'extent') = 'array' THEN geometry -> 'extent'
                        WHEN jsonb_typeof(geometry -> 'extent') = 'object'
                         AND geometry -> 'extent' ?& ARRAY['minX', 'minY', 'maxX', 'maxY']
                        THEN jsonb_build_array(
                            (geometry -> 'extent' ->> 'minX')::numeric,
                            (geometry -> 'extent' ->> 'minY')::numeric,
                            (geometry -> 'extent' ->> 'maxX')::numeric,
                            (geometry -> 'extent' ->> 'maxY')::numeric
                        )
                    END
                ))
            END
        )) AS snapshot
    FROM sql_sources
)
UPDATE service.query_services qs
SET data_config = (qs.data_config - 'geometry' - 'object_table')
    || jsonb_build_object('source_snapshot', sql_snapshots.snapshot)
FROM sql_snapshots
WHERE qs.id = sql_snapshots.id;

COMMENT ON COLUMN service.query_services.data_config IS
'查询服务配置：locator、source_snapshot、default_fields、filterable_fields；不再使用 geometry/object_table';
