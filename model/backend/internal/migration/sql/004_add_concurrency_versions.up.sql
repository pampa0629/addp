ALTER TABLE model.entities
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1
        CONSTRAINT ck_model_entities_version_positive CHECK (version > 0);

ALTER TABLE model.logical_tables
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1
        CONSTRAINT ck_model_logical_tables_version_positive CHECK (version > 0);

ALTER TABLE model.dw_layers
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1
        CONSTRAINT ck_model_dw_layers_version_positive CHECK (version > 0);

ALTER TABLE model.entity_relations
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1
        CONSTRAINT ck_model_entity_relations_version_positive CHECK (version > 0);

CREATE TABLE model.entity_model_revisions (
    tenant_id BIGINT PRIMARY KEY,
    revision BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_model_entity_model_revision_positive CHECK (revision > 0)
);

INSERT INTO model.entity_model_revisions (tenant_id)
SELECT tenant_id FROM model.entities
UNION
SELECT tenant_id FROM model.entity_relations
ON CONFLICT (tenant_id) DO NOTHING;
