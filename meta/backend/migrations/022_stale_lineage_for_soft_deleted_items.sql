UPDATE meta.lineage_item_relations AS relation
SET status = 'stale',
    updated_at = NOW()
WHERE relation.status <> 'closed'
  AND EXISTS (
      SELECT 1
      FROM meta.meta_item AS item
      WHERE item.tenant_id = relation.tenant_id
        AND item.deleted_at IS NOT NULL
        AND item.id IN (relation.source_item_id, relation.target_item_id)
  );

UPDATE meta.lineage_service_dependencies AS dependency
SET status = 'stale',
    updated_at = NOW()
WHERE dependency.status <> 'closed'
  AND EXISTS (
      SELECT 1
      FROM meta.meta_item AS item
      WHERE item.tenant_id = dependency.tenant_id
        AND item.deleted_at IS NOT NULL
        AND item.id = dependency.source_item_id
  );

CREATE OR REPLACE FUNCTION meta.stale_lineage_for_soft_deleted_item()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE meta.lineage_item_relations
    SET status = 'stale',
        updated_at = NOW()
    WHERE tenant_id = NEW.tenant_id
      AND status <> 'closed'
      AND (source_item_id = NEW.id OR target_item_id = NEW.id);

    UPDATE meta.lineage_service_dependencies
    SET status = 'stale',
        updated_at = NOW()
    WHERE tenant_id = NEW.tenant_id
      AND status <> 'closed'
      AND source_item_id = NEW.id;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_meta_item_stale_lineage ON meta.meta_item;
CREATE TRIGGER trg_meta_item_stale_lineage
AFTER UPDATE OF deleted_at ON meta.meta_item
FOR EACH ROW
WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
EXECUTE FUNCTION meta.stale_lineage_for_soft_deleted_item();
