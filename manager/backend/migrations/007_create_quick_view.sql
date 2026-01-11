-- 创建 quick_view 表（快显/预缓存管理）
-- 用于管理空间数据表的瓦片预缓存任务状态和配置

CREATE TABLE IF NOT EXISTS manager.quick_view (
    id SERIAL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    engine_id INTEGER NOT NULL,
    schema_name VARCHAR(255) NOT NULL,
    table_name VARCHAR(255) NOT NULL,

    -- 快显状态
    status VARCHAR(50) NOT NULL DEFAULT 'none',  -- none, generating, ready, failed, cancelled
    error_message TEXT,

    -- 缓存配置
    min_zoom INTEGER,                            -- 自动计算得到
    max_zoom INTEGER DEFAULT 18,
    actual_max_zoom INTEGER,                     -- 实际生成到的最大级别

    -- 统计信息
    total_tiles INTEGER DEFAULT 0,
    cached_tiles INTEGER DEFAULT 0,
    last_zoom_avg_time_ms FLOAT,                 -- 最后一层平均生成时间（毫秒）
    last_zoom_avg_size_kb FLOAT,                 -- 最后一层平均瓦片大小（KB）

    -- 停止条件配置
    stop_threshold_time_ms FLOAT DEFAULT 300,    -- 停止阈值：生成时间（毫秒）
    stop_threshold_size_kb FLOAT DEFAULT 100,    -- 停止阈值：瓦片大小（KB）

    -- 指纹（用于 MinIO 路径）
    fingerprint VARCHAR(64) NOT NULL,

    -- 空间范围（存储快照，避免依赖 meta）
    extent JSONB,                                -- [minLng, minLat, maxLng, maxLat]
    extent_srid INTEGER DEFAULT 4326,            -- extent 的坐标系

    -- 时间戳
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_quick_view_tenant_resource ON manager.quick_view(tenant_id, engine_id);
CREATE INDEX IF NOT EXISTS idx_quick_view_status ON manager.quick_view(status);
CREATE INDEX IF NOT EXISTS idx_quick_view_fingerprint ON manager.quick_view(fingerprint);

-- 添加唯一约束（一个表只能有一个快显任务）
CREATE UNIQUE INDEX IF NOT EXISTS idx_quick_view_unique_resource ON manager.quick_view(tenant_id, engine_id, schema_name, table_name);

-- 添加注释
COMMENT ON TABLE manager.quick_view IS '快显（预缓存）任务管理表';
COMMENT ON COLUMN manager.quick_view.id IS '快显任务唯一标识';
COMMENT ON COLUMN manager.quick_view.tenant_id IS '租户 ID';
COMMENT ON COLUMN manager.quick_view.engine_id IS '引擎 ID';
COMMENT ON COLUMN manager.quick_view.schema_name IS 'Schema 名称';
COMMENT ON COLUMN manager.quick_view.table_name IS '表名';
COMMENT ON COLUMN manager.quick_view.status IS '任务状态：none, generating, ready, failed, cancelled';
COMMENT ON COLUMN manager.quick_view.error_message IS '错误信息（失败时）';
COMMENT ON COLUMN manager.quick_view.min_zoom IS '最小缩放级别（自动计算）';
COMMENT ON COLUMN manager.quick_view.max_zoom IS '最大缩放级别';
COMMENT ON COLUMN manager.quick_view.actual_max_zoom IS '实际生成到的最大级别';
COMMENT ON COLUMN manager.quick_view.total_tiles IS '总瓦片数（估算）';
COMMENT ON COLUMN manager.quick_view.cached_tiles IS '已缓存瓦片数';
COMMENT ON COLUMN manager.quick_view.last_zoom_avg_time_ms IS '最后一层平均生成时间（毫秒）';
COMMENT ON COLUMN manager.quick_view.last_zoom_avg_size_kb IS '最后一层平均瓦片大小（KB）';
COMMENT ON COLUMN manager.quick_view.stop_threshold_time_ms IS '停止阈值：生成时间（毫秒）';
COMMENT ON COLUMN manager.quick_view.stop_threshold_size_kb IS '停止阈值：瓦片大小（KB）';
COMMENT ON COLUMN manager.quick_view.fingerprint IS '表指纹（用于 MinIO 路径）';
COMMENT ON COLUMN manager.quick_view.extent IS '空间范围 [minLng, minLat, maxLng, maxLat]';
COMMENT ON COLUMN manager.quick_view.extent_srid IS '坐标参考系统';
COMMENT ON COLUMN manager.quick_view.started_at IS '任务开始时间';
COMMENT ON COLUMN manager.quick_view.completed_at IS '任务完成时间';
COMMENT ON COLUMN manager.quick_view.created_at IS '创建时间';
COMMENT ON COLUMN manager.quick_view.updated_at IS '更新时间';
