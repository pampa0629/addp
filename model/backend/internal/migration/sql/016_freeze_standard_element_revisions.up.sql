ALTER TABLE model.entity_attributes
    ADD COLUMN IF NOT EXISTS element_revision_id BIGINT;

ALTER TABLE model.logical_fields
    ADD COLUMN IF NOT EXISTS element_revision_id BIGINT;

CREATE INDEX IF NOT EXISTS idx_model_entity_attributes_element_revision
    ON model.entity_attributes(element_revision_id)
    WHERE element_revision_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_model_logical_fields_element_revision
    ON model.logical_fields(element_revision_id)
    WHERE element_revision_id IS NOT NULL;

-- Existing approved aggregates cannot be assigned a historical Standard
-- revision deterministically during a database-only migration. Reopen only
-- affected aggregates and require an explicit approval to create the snapshot.
UPDATE model.entities AS entity
SET status = 'draft', version = entity.version + 1, updated_at = NOW()
WHERE entity.status = 'approved'
  AND EXISTS (
      SELECT 1 FROM model.entity_attributes AS attribute
      WHERE attribute.entity_id = entity.id AND attribute.element_id IS NOT NULL
  );

UPDATE model.logical_tables AS logical_table
SET status = 'draft', version = logical_table.version + 1, updated_at = NOW()
WHERE logical_table.status = 'approved'
  AND EXISTS (
      SELECT 1 FROM model.logical_fields AS field
      WHERE field.table_id = logical_table.id AND field.element_id IS NOT NULL
  );

ALTER TABLE model.entity_attributes
    DROP CONSTRAINT IF EXISTS ck_model_entity_attribute_element_revision_pair;
ALTER TABLE model.entity_attributes
    ADD CONSTRAINT ck_model_entity_attribute_element_revision_pair
    CHECK (element_revision_id IS NULL OR element_id IS NOT NULL);

ALTER TABLE model.logical_fields
    DROP CONSTRAINT IF EXISTS ck_model_logical_field_element_revision_pair;
ALTER TABLE model.logical_fields
    ADD CONSTRAINT ck_model_logical_field_element_revision_pair
    CHECK (element_revision_id IS NULL OR element_id IS NOT NULL);

CREATE OR REPLACE FUNCTION model.enforce_entity_element_revision_snapshot()
RETURNS TRIGGER LANGUAGE plpgsql AS $function$
BEGIN
    IF NEW.status = 'approved' AND EXISTS (
        SELECT 1 FROM model.entity_attributes AS attribute
        WHERE attribute.entity_id = NEW.id
          AND attribute.element_id IS NOT NULL
          AND attribute.element_revision_id IS NULL
    ) THEN
        RAISE EXCEPTION 'approved entity contains an unfrozen data element reference' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$function$;

DROP TRIGGER IF EXISTS trg_model_entity_element_revision_snapshot ON model.entities;
CREATE TRIGGER trg_model_entity_element_revision_snapshot
BEFORE INSERT OR UPDATE OF status ON model.entities
FOR EACH ROW EXECUTE FUNCTION model.enforce_entity_element_revision_snapshot();

CREATE OR REPLACE FUNCTION model.enforce_entity_attribute_element_revision_snapshot()
RETURNS TRIGGER LANGUAGE plpgsql AS $function$
DECLARE
    parent_status TEXT;
BEGIN
    SELECT status INTO parent_status FROM model.entities WHERE id = NEW.entity_id;
    IF parent_status = 'approved'
       AND NEW.element_id IS NOT NULL
       AND NEW.element_revision_id IS NULL THEN
        RAISE EXCEPTION 'approved entity attribute must freeze its data element revision' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$function$;

DROP TRIGGER IF EXISTS trg_model_entity_attribute_element_revision_snapshot ON model.entity_attributes;
CREATE TRIGGER trg_model_entity_attribute_element_revision_snapshot
BEFORE INSERT OR UPDATE OF entity_id, element_id, element_revision_id ON model.entity_attributes
FOR EACH ROW EXECUTE FUNCTION model.enforce_entity_attribute_element_revision_snapshot();

CREATE OR REPLACE FUNCTION model.enforce_logical_table_element_revision_snapshot()
RETURNS TRIGGER LANGUAGE plpgsql AS $function$
BEGIN
    IF NEW.status = 'approved' AND EXISTS (
        SELECT 1 FROM model.logical_fields AS field
        WHERE field.table_id = NEW.id
          AND field.element_id IS NOT NULL
          AND field.element_revision_id IS NULL
    ) THEN
        RAISE EXCEPTION 'approved logical table contains an unfrozen data element reference' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$function$;

DROP TRIGGER IF EXISTS trg_model_logical_table_element_revision_snapshot ON model.logical_tables;
CREATE TRIGGER trg_model_logical_table_element_revision_snapshot
BEFORE INSERT OR UPDATE OF status ON model.logical_tables
FOR EACH ROW EXECUTE FUNCTION model.enforce_logical_table_element_revision_snapshot();

CREATE OR REPLACE FUNCTION model.enforce_logical_field_element_revision_snapshot()
RETURNS TRIGGER LANGUAGE plpgsql AS $function$
DECLARE
    parent_status TEXT;
BEGIN
    SELECT status INTO parent_status FROM model.logical_tables WHERE id = NEW.table_id;
    IF parent_status = 'approved'
       AND NEW.element_id IS NOT NULL
       AND NEW.element_revision_id IS NULL THEN
        RAISE EXCEPTION 'approved logical field must freeze its data element revision' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$function$;

DROP TRIGGER IF EXISTS trg_model_logical_field_element_revision_snapshot ON model.logical_fields;
CREATE TRIGGER trg_model_logical_field_element_revision_snapshot
BEFORE INSERT OR UPDATE OF table_id, element_id, element_revision_id ON model.logical_fields
FOR EACH ROW EXECUTE FUNCTION model.enforce_logical_field_element_revision_snapshot();
