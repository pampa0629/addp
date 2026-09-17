-- Normalize the logical vocabulary; physical TEXT remains an engine mapping.
UPDATE model.entity_model_revisions SET revision=revision+1
WHERE tenant_id IN (SELECT tenant_id FROM model.entities WHERE id IN
    (SELECT entity_id FROM model.entity_attributes WHERE data_type='text'));
UPDATE model.entities SET version=version+1
WHERE id IN (SELECT entity_id FROM model.entity_attributes WHERE data_type='text');
UPDATE model.logical_tables SET version=version+1
WHERE id IN (SELECT table_id FROM model.logical_fields WHERE data_type='text');
UPDATE model.entity_attributes SET data_type='string' WHERE data_type='text';
UPDATE model.logical_fields SET data_type='string', length=NULL WHERE data_type='text';
ALTER TABLE model.entity_attributes DROP CONSTRAINT ck_model_entity_attribute_type;
ALTER TABLE model.entity_attributes ADD CONSTRAINT ck_model_entity_attribute_type
 CHECK (data_type IN ('string','int','bigint','float','decimal','date','datetime','bool','json','geometry'));
ALTER TABLE model.logical_fields DROP CONSTRAINT ck_model_logical_field_type;
ALTER TABLE model.logical_fields ADD CONSTRAINT ck_model_logical_field_type
 CHECK (data_type IN ('string','int','bigint','float','decimal','date','datetime','bool','json','geometry'));
