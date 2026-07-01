DO $$
BEGIN
    IF to_regclass('manager.preview_state') IS NULL
       AND to_regclass('manager.quick_view') IS NOT NULL THEN
        ALTER TABLE manager.quick_view RENAME TO preview_state;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS manager.preview_state (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    item_fingerprint VARCHAR(64) NOT NULL,
    locator TEXT NOT NULL,
    preferred_mode VARCHAR(32) DEFAULT 'basic_preview',
    view_state JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE manager.preview_state
    ADD COLUMN IF NOT EXISTS item_fingerprint VARCHAR(64),
    ADD COLUMN IF NOT EXISTS locator TEXT,
    ADD COLUMN IF NOT EXISTS preferred_mode VARCHAR(32) DEFAULT 'basic_preview',
    ADD COLUMN IF NOT EXISTS view_state JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

UPDATE manager.preview_state
SET preferred_mode = 'basic_preview'
WHERE preferred_mode IS NULL OR preferred_mode = '';

DROP INDEX IF EXISTS manager.idx_quick_view_tenant_fingerprint;
CREATE UNIQUE INDEX IF NOT EXISTS idx_preview_state_tenant_fingerprint
    ON manager.preview_state (tenant_id, item_fingerprint);

DROP TABLE IF EXISTS manager.quick_view;

COMMENT ON TABLE manager.preview_state IS 'Manager 数据项预览状态表，保存预览模式偏好和 basic_preview / quick_view 的交互视角状态';
COMMENT ON COLUMN manager.preview_state.view_state IS '预览交互状态。顶层按显示模式分为 basic_preview 和 quick_view，模式内按渲染域保存 map 地图视口和 scene_3d 三维相机状态';
