DROP TRIGGER IF EXISTS trg_meta_item_data_item_change ON meta.meta_item;
DROP FUNCTION IF EXISTS meta.capture_data_item_change();
DROP TABLE IF EXISTS meta.data_item_changes;
