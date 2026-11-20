CREATE TABLE IF NOT EXISTS project_group_memberships (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    project_group_id BIGINT NOT NULL,
    tenant_membership_id BIGINT NOT NULL,
    relation_role VARCHAR(16) NOT NULL DEFAULT 'member' CHECK (relation_role IN ('member','leader','coordinator')),
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active','ended')),
    ended_at DATETIME(6),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version>0),
    active_member BIGINT GENERATED ALWAYS AS (IF(status='active',tenant_membership_id,NULL)) STORED,
    UNIQUE (project_group_id,active_member),
    KEY (tenant_id,tenant_membership_id,status),
    FOREIGN KEY (tenant_id,project_group_id) REFERENCES project_groups(tenant_id,id),
    FOREIGN KEY (tenant_id,tenant_membership_id) REFERENCES tenant_memberships(tenant_id,id),
    CHECK ((status='ended')=(ended_at IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
