ALTER TABLE meta.lineage_item_relations
    ADD COLUMN source_field_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN target_field_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN source_schema_hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN target_schema_hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN transformation TEXT NOT NULL DEFAULT '';
ALTER TABLE meta.lineage_observations
    ADD COLUMN source_field_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN target_field_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN source_schema_hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN target_schema_hash TEXT NOT NULL DEFAULT '';

ALTER TABLE meta.lineage_item_relations DROP CONSTRAINT lineage_item_relations_distinct_items;
ALTER TABLE meta.lineage_item_relations ADD CONSTRAINT lineage_relation_endpoints CHECK (
    (granularity = 'item' AND source_item_id <> target_item_id
        AND source_field_name = '' AND target_field_name = ''
        AND source_schema_hash = '' AND target_schema_hash = '' AND transformation = '')
    OR (granularity = 'field' AND source_field_name <> '' AND target_field_name <> ''
        AND source_schema_hash <> '' AND target_schema_hash <> ''
        AND transformation IN ('direct', 'derived'))
);
DROP INDEX meta.uq_lineage_item_relations_active;
CREATE UNIQUE INDEX uq_lineage_item_relations_active
    ON meta.lineage_item_relations (tenant_id, source_item_id, target_item_id, relation_kind)
    WHERE status <> 'closed' AND granularity = 'item';
CREATE UNIQUE INDEX uq_lineage_field_relations_active
    ON meta.lineage_item_relations (tenant_id, source_item_id, source_field_name, source_schema_hash,
        target_item_id, target_field_name, target_schema_hash, relation_kind)
    WHERE status <> 'closed' AND granularity = 'field';
CREATE INDEX idx_lineage_field_relations_source
    ON meta.lineage_item_relations (tenant_id, source_item_id, source_field_name, source_schema_hash, status)
    WHERE granularity = 'field';
CREATE INDEX idx_lineage_field_relations_target
    ON meta.lineage_item_relations (tenant_id, target_item_id, target_field_name, target_schema_hash, status)
    WHERE granularity = 'field';
CREATE UNIQUE INDEX uq_lineage_field_observation_execution
    ON meta.lineage_observations (tenant_id, execution_id, source_item_id, source_field_name,
        source_schema_hash, target_item_id, target_field_name, target_schema_hash, relation_kind)
    WHERE granularity = 'field' AND execution_id IS NOT NULL;

CREATE INDEX idx_lineage_item_observation_target_latest
    ON meta.lineage_observations (tenant_id, target_item_id, observed_at DESC, id DESC)
    WHERE granularity = 'item';
CREATE INDEX idx_lineage_item_observation_source_latest
    ON meta.lineage_observations (tenant_id, source_item_id, observed_at DESC, id DESC)
    WHERE granularity = 'item';

CREATE FUNCTION meta.lineage_structural_fields(attrs JSONB)
RETURNS JSONB LANGUAGE SQL IMMUTABLE AS $$
    SELECT COALESCE(jsonb_agg(jsonb_build_object(
        'path', COALESCE(NULLIF(field->'path', '[]'::jsonb), jsonb_build_array(field->>'name')),
        'type', field->'type', 'element_type', field->'element_type',
        'nullable', COALESCE(field->'nullable', 'false'::jsonb)
    ) ORDER BY COALESCE(NULLIF(field->'path', '[]'::jsonb), jsonb_build_array(field->>'name'))::text), '[]'::jsonb)
    FROM jsonb_array_elements(COALESCE(attrs #> '{type_info,table,fields}', '[]'::jsonb)) AS field;
$$;
CREATE FUNCTION meta.stale_lineage_for_changed_fields()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF meta.lineage_structural_fields(OLD.attributes)
        IS DISTINCT FROM meta.lineage_structural_fields(NEW.attributes) THEN
        UPDATE meta.lineage_item_relations SET status = 'stale', updated_at = NOW()
        WHERE tenant_id = NEW.tenant_id AND granularity = 'field' AND status <> 'closed'
            AND (source_item_id = NEW.id OR target_item_id = NEW.id);
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_meta_item_stale_field_lineage
AFTER UPDATE OF attributes ON meta.meta_item
FOR EACH ROW EXECUTE FUNCTION meta.stale_lineage_for_changed_fields();
