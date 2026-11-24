CREATE TABLE IF NOT EXISTS manager.quick_view_policy (
    id bigint unsigned PRIMARY KEY CHECK (id = 1),
    version bigint unsigned NOT NULL,
    direct_flat_geobuf_max_rows bigint NOT NULL,
    realtime_tile_timeout_ms bigint NOT NULL,
    realtime_tile_retry_after_sec bigint NOT NULL,
    raster_mosaic_generation_timeout_sec bigint NOT NULL DEFAULT 7200,
    updated_by bigint unsigned NOT NULL, created_at datetime(6), updated_at datetime(6)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS manager.base_map_providers (
    id bigint unsigned AUTO_INCREMENT PRIMARY KEY, version bigint unsigned NOT NULL,
    scope_type varchar(20) NOT NULL, tenant_id bigint unsigned, provider varchar(30) NOT NULL,
    enabled boolean NOT NULL, sort_order bigint NOT NULL, a_map_key text, a_map_security_js_code text,
    tdt_key text, updated_by bigint unsigned NOT NULL, created_at datetime(6), updated_at datetime(6),
    scope_tenant bigint unsigned GENERATED ALWAYS AS (COALESCE(tenant_id,0)) VIRTUAL,
    UNIQUE KEY uq_manager_base_map_scope_provider (scope_type,scope_tenant,provider)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
