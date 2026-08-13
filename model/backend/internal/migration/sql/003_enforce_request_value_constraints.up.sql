ALTER TABLE model.entities
    ADD CONSTRAINT ck_model_entity_domain_id CHECK (domain_id IS NULL OR domain_id > 0);

ALTER TABLE model.entity_attributes
    ADD CONSTRAINT ck_model_entity_attribute_element_id CHECK (element_id IS NULL OR element_id > 0),
    ADD CONSTRAINT ck_model_entity_attribute_sort_order CHECK (sort_order >= 0);

ALTER TABLE model.logical_tables
    ADD CONSTRAINT ck_model_logical_table_domain_id CHECK (domain_id IS NULL OR domain_id > 0),
    ADD CONSTRAINT ck_model_logical_table_entity_id CHECK (entity_id IS NULL OR entity_id > 0);

ALTER TABLE model.logical_fields
    ADD CONSTRAINT ck_model_logical_field_element_id CHECK (element_id IS NULL OR element_id > 0),
    ADD CONSTRAINT ck_model_logical_field_hierarchy_id CHECK (hierarchy_id IS NULL OR hierarchy_id > 0),
    ADD CONSTRAINT ck_model_logical_field_sort_order CHECK (sort_order >= 0);

ALTER TABLE model.fact_metric_mappings
    ADD CONSTRAINT ck_model_fact_metric_metric_id CHECK (metric_id > 0),
    ADD CONSTRAINT ck_model_fact_metric_field_id CHECK (field_id IS NULL OR field_id > 0);
