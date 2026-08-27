CREATE TABLE IF NOT EXISTS meta.data_item_changes (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    item_id BIGINT NOT NULL,
    source_identity VARCHAR(64) NOT NULL,
    operation VARCHAR(16) NOT NULL CHECK (operation IN ('upsert', 'missing')),
    snapshot JSONB NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_data_item_changes_tenant_cursor
    ON meta.data_item_changes (tenant_id, id);
CREATE INDEX IF NOT EXISTS idx_data_item_changes_source
    ON meta.data_item_changes (tenant_id, source_identity, id DESC);

INSERT INTO meta.data_item_changes (
    tenant_id,
    item_id,
    source_identity,
    operation,
    snapshot,
    observed_at
)
SELECT
    item.tenant_id,
    item.id,
    item.fingerprint,
    CASE WHEN item.deleted_at IS NULL THEN 'upsert' ELSE 'missing' END,
    jsonb_strip_nulls(jsonb_build_object(
        'item_id', item.id,
        'engine_id', item.engine_id,
        'node_id', item.node_id,
        'item_type', item.item_type,
        'name', item.name,
        'full_name', item.full_name,
        'row_count', item.row_count,
        'size_bytes', item.size_bytes,
        'data_updated_at', item.data_updated_at,
        'scanned_at', item.scanned_at,
        'scanned_depth', item.scanned_depth,
        'fields', item.attributes #> '{type_info,table,fields}'
    )),
    COALESCE(item.scanned_at, item.created_at, NOW())
FROM meta.meta_item AS item
ORDER BY item.id;

CREATE OR REPLACE FUNCTION meta.capture_data_item_change()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    changed_item meta.meta_item%ROWTYPE;
    change_operation VARCHAR(16);
BEGIN
    IF TG_OP = 'DELETE' THEN
        changed_item := OLD;
        change_operation := 'missing';
    ELSE
        changed_item := NEW;
        change_operation := CASE
            WHEN NEW.deleted_at IS NULL THEN 'upsert'
            ELSE 'missing'
        END;
    END IF;

    INSERT INTO meta.data_item_changes (
        tenant_id,
        item_id,
        source_identity,
        operation,
        snapshot,
        observed_at
    ) VALUES (
        changed_item.tenant_id,
        changed_item.id,
        changed_item.fingerprint,
        change_operation,
        jsonb_strip_nulls(jsonb_build_object(
            'item_id', changed_item.id,
            'engine_id', changed_item.engine_id,
            'node_id', changed_item.node_id,
            'item_type', changed_item.item_type,
            'name', changed_item.name,
            'full_name', changed_item.full_name,
            'row_count', changed_item.row_count,
            'size_bytes', changed_item.size_bytes,
            'data_updated_at', changed_item.data_updated_at,
            'scanned_at', changed_item.scanned_at,
            'scanned_depth', changed_item.scanned_depth,
            'fields', changed_item.attributes #> '{type_info,table,fields}'
        )),
        NOW()
    );

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_meta_item_data_item_change ON meta.meta_item;
CREATE TRIGGER trg_meta_item_data_item_change
AFTER INSERT OR UPDATE OR DELETE ON meta.meta_item
FOR EACH ROW
EXECUTE FUNCTION meta.capture_data_item_change();
