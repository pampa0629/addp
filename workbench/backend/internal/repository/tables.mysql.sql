CREATE TABLE IF NOT EXISTS data_applications (
    id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    owner_user_id BIGINT NOT NULL,
    name VARCHAR(200) NOT NULL,
    description TEXT NOT NULL,
    draft_snapshot JSON NOT NULL,
    draft_content_hash VARCHAR(71) NOT NULL,
    publication_status VARCHAR(32) NOT NULL,
    current_revision_number BIGINT,
    current_revision_hash VARCHAR(71) NOT NULL,
    version BIGINT NOT NULL,
    created_at DATETIME(6),
    updated_at DATETIME(6),
    UNIQUE KEY ux_workbench_data_applications_id_tenant (id, tenant_id),
    KEY idx_workbench_applications_owner_updated (tenant_id, owner_user_id, updated_at DESC, id),
    CHECK (version > 0),
    CHECK ((publication_status = 'unpublished' AND current_revision_number IS NULL AND current_revision_hash = '')
        OR (publication_status IN ('published','offline') AND current_revision_number > 0 AND current_revision_hash LIKE 'sha256:%'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS data_application_revisions (
    id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    application_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    tenant_id BIGINT NOT NULL,
    revision_number BIGINT NOT NULL,
    name VARCHAR(200) NOT NULL,
    description TEXT NOT NULL,
    snapshot JSON NOT NULL,
    content_hash VARCHAR(71) NOT NULL,
    published_by BIGINT NOT NULL,
    published_at DATETIME(6) NOT NULL,
    UNIQUE KEY ux_workbench_application_revision (application_id, revision_number),
    KEY idx_workbench_application_revisions_current (tenant_id, application_id, revision_number DESC),
    FOREIGN KEY (application_id, tenant_id) REFERENCES data_applications(id, tenant_id) ON DELETE RESTRICT,
    CHECK (revision_number > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS catalog_resource_changes (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    source_type VARCHAR(32) NOT NULL,
    source_identity CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    operation VARCHAR(16) NOT NULL,
    snapshot JSON NOT NULL,
    observed_at DATETIME(6) NOT NULL,
    KEY idx_workbench_catalog_changes_tenant_id (tenant_id, id),
    KEY idx_workbench_catalog_changes_source (tenant_id, source_type, source_identity, id DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS resource_access_rules (
    id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    resource_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    subject_type VARCHAR(32) NOT NULL,
    subject_id BIGINT NOT NULL,
    permission VARCHAR(128) NOT NULL,
    effect VARCHAR(16) NOT NULL,
    source_module VARCHAR(64) NOT NULL,
    source_identity VARCHAR(128) NOT NULL,
    expires_at DATETIME(6),
    revoked_at DATETIME(6),
    created_at DATETIME(6),
    updated_at DATETIME(6),
    UNIQUE KEY ux_workbench_resource_rule_source (tenant_id, source_module, source_identity),
    KEY idx_workbench_resource_rule_runtime (tenant_id, resource_type, resource_id, subject_type, subject_id, permission, effect),
    CHECK (resource_type = 'data_application'),
    CHECK (subject_type = 'user' AND subject_id > 0),
    CHECK (permission = 'workbench.data_application.execute'),
    CHECK (effect IN ('allow','deny')),
    CHECK (source_module = 'asset' AND REGEXP_LIKE(source_identity, '^[1-9][0-9]*$'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
