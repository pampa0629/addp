CREATE TABLE IF NOT EXISTS manager.preview_state (
    id bigserial PRIMARY KEY,
    tenant_id bigint NOT NULL,
    item_fingerprint varchar(64) NOT NULL,
    locator text NOT NULL,
    preferred_mode varchar(32) DEFAULT 'basic_preview',
    view_state jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz,
    updated_at timestamptz
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_attribute
        WHERE attrelid = 'manager.preview_state'::regclass
          AND attname = 'user_id' AND NOT attisdropped
    ) THEN
        LOCK TABLE manager.preview_state IN ACCESS EXCLUSIVE MODE;
        -- Shared preferences have no reliable owner.
        DELETE FROM manager.preview_state;
        ALTER TABLE manager.preview_state ADD COLUMN user_id bigint NOT NULL
            CONSTRAINT chk_preview_state_user_id CHECK (user_id > 0);
        DROP INDEX IF EXISTS manager.idx_preview_state_tenant_fingerprint;
        CREATE UNIQUE INDEX idx_preview_state_tenant_item_user
            ON manager.preview_state (tenant_id, item_fingerprint, user_id);
    END IF;
END $$;
