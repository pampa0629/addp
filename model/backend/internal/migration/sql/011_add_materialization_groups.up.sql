CREATE TABLE model.materialization_groups (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    code VARCHAR(100) NOT NULL,
    name VARCHAR(200) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL,
    updated_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_model_materialization_groups_code CHECK (code ~ '^[a-z][a-z0-9_]*$'),
    CONSTRAINT ck_model_materialization_groups_version CHECK (version > 0),
    CONSTRAINT uq_model_materialization_groups_tenant_code UNIQUE (tenant_id, code),
    CONSTRAINT uq_model_materialization_groups_id_tenant UNIQUE (id, tenant_id)
);

CREATE TABLE model.materialization_group_members (
    group_id BIGINT NOT NULL,
    tenant_id BIGINT NOT NULL,
    logical_table_id BIGINT NOT NULL,
    position INTEGER NOT NULL,
    PRIMARY KEY (group_id, logical_table_id),
    CONSTRAINT ck_model_materialization_group_members_position CHECK (position >= 0),
    CONSTRAINT uq_model_materialization_group_members_position UNIQUE (group_id, position),
    CONSTRAINT fk_model_materialization_group_members_group
        FOREIGN KEY (group_id, tenant_id)
        REFERENCES model.materialization_groups(id, tenant_id)
        ON DELETE CASCADE,
    CONSTRAINT fk_model_materialization_group_members_logical_table
        FOREIGN KEY (logical_table_id, tenant_id)
        REFERENCES model.logical_tables(id, tenant_id)
        ON DELETE RESTRICT
);

CREATE INDEX idx_model_materialization_groups_list
    ON model.materialization_groups(tenant_id, updated_at DESC, id DESC);
CREATE INDEX idx_model_materialization_group_members_table
    ON model.materialization_group_members(tenant_id, logical_table_id);
