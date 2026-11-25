CREATE TABLE IF NOT EXISTS applications (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `tenant_id` bigint NOT NULL,
    `asset_id` bigint NOT NULL,
    `applicant_id` bigint NOT NULL,
    `reason` varchar(2000),
    `duration_day` bigint DEFAULT 30,
    `status` varchar(50) DEFAULT 'pending' NOT NULL,
    `reviewer_id` bigint,
    `review_note` varchar(2000),
    `reviewed_at` datetime(6),
    `expires_at` datetime(6),
    `created_at` datetime(6),
    `updated_at` datetime(6),
    PRIMARY KEY (id),
    KEY idx_asset_applications_applicant_id (applicant_id),
    KEY idx_asset_applications_asset_id (asset_id),
    KEY idx_asset_applications_status (status),
    KEY idx_asset_applications_tenant_id (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS asset_components (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `tenant_id` bigint NOT NULL,
    `asset_id` bigint NOT NULL,
    `catalog_entry_id` CHAR(36) NOT NULL,
    `role` varchar(16) NOT NULL,
    `sort_order` bigint DEFAULT 0 NOT NULL,
    `created_at` datetime(6),
    `updated_at` datetime(6),
    CONSTRAINT ck_asset_component_role CHECK (role IN ('primary', 'supporting')),
    PRIMARY KEY (id),
    KEY idx_asset_asset_components_asset_id (asset_id),
    KEY idx_asset_asset_components_tenant_id (tenant_id),
    UNIQUE KEY uq_asset_component_entry (tenant_id, asset_id, catalog_entry_id),
    primary_asset_id BIGINT GENERATED ALWAYS AS (IF(role = 'primary', asset_id, NULL)) STORED,
    UNIQUE KEY uq_asset_component_primary (tenant_id, primary_asset_id),
    CONSTRAINT ck_asset_component_uuid CHECK (IS_UUID(catalog_entry_id) AND LENGTH(catalog_entry_id)=36)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS asset_ext_fields (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `asset_id` bigint NOT NULL,
    `field_key` varchar(100) NOT NULL,
    `value` json,
    `created_at` datetime(6),
    `updated_at` datetime(6),
    PRIMARY KEY (id),
    KEY idx_asset_asset_ext_fields_asset_id (asset_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS assets (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `tenant_id` bigint NOT NULL,
    `name` varchar(500) NOT NULL,
    `description` varchar(2000),
    `type_id` bigint NOT NULL,
    `category_id` bigint,
    `tags` json DEFAULT ('[]'),
    `status` varchar(50) DEFAULT 'draft' NOT NULL,
    `owner_id` bigint NOT NULL,
    `version` bigint DEFAULT 1 NOT NULL,
    `published_at` datetime(6),
    `created_by` bigint NOT NULL,
    `updated_by` bigint,
    `created_at` datetime(6),
    `updated_at` datetime(6),
    PRIMARY KEY (id),
    KEY idx_asset_assets_category_id (category_id),
    KEY idx_asset_assets_owner_id (owner_id),
    KEY idx_asset_assets_status (status),
    KEY idx_asset_assets_tenant_id (tenant_id),
    KEY idx_asset_assets_type_id (type_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS authorizations (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `tenant_id` bigint NOT NULL,
    `asset_id` bigint NOT NULL,
    `application_id` bigint NOT NULL,
    `user_id` bigint NOT NULL,
    `status` varchar(32) DEFAULT 'pending' NOT NULL,
    `target_module` varchar(64) DEFAULT '' NOT NULL,
    `target_resource_type` varchar(64) DEFAULT '' NOT NULL,
    `target_resource_id` varchar(128) DEFAULT '' NOT NULL,
    `expires_at` datetime(6),
    `fulfillment_attempt` bigint DEFAULT 0 NOT NULL,
    `fulfillment_last_error` varchar(2000) DEFAULT '' NOT NULL,
    `next_attempt_at` datetime(6),
    `fulfilled_at` datetime(6),
    `revoked_at` datetime(6),
    `revoked_by` bigint,
    `created_at` datetime(6),
    `updated_at` datetime(6),
    CONSTRAINT ck_asset_authorization_lifecycle CHECK (((((status) = 'pending') AND (revoked_at IS NULL)) OR (((status) = 'effective') AND ((target_resource_id) <> '') AND (fulfilled_at IS NOT NULL) AND (revoked_at IS NULL)) OR (((status) = 'revocation_pending') AND (revoked_at IS NULL)) OR (((status) = 'revoked') AND ((target_resource_id) <> '') AND (revoked_at IS NOT NULL)))),
    CONSTRAINT ck_asset_authorization_status CHECK (status IN ('pending', 'effective', 'revocation_pending', 'revoked')),
    CONSTRAINT ck_asset_authorization_target CHECK (((((target_module) = '') AND ((target_resource_type) = '') AND ((target_resource_id) = '')) OR (((target_module) = 'workbench') AND ((target_resource_type) = 'data_application') AND ((target_resource_id) REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')))),
    PRIMARY KEY (id),
    UNIQUE KEY idx_asset_authorizations_application_id (application_id),
    KEY idx_asset_authorizations_asset_id (asset_id),
    KEY idx_asset_authorizations_next_attempt_at (next_attempt_at),
    KEY idx_asset_authorizations_status (status),
    KEY idx_asset_authorizations_tenant_id (tenant_id),
    KEY idx_asset_authorizations_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS categories (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `tenant_id` bigint NOT NULL,
    `name` varchar(200) NOT NULL,
    `parent_id` bigint,
    `sort_order` bigint DEFAULT 0,
    `description` varchar(500),
    `version` bigint DEFAULT 1 NOT NULL,
    `created_at` datetime(6),
    `updated_at` datetime(6),
    PRIMARY KEY (id),
    KEY idx_asset_categories_parent_id (parent_id),
    KEY idx_asset_categories_tenant_id (tenant_id),
    UNIQUE KEY idx_asset_categories_unique_child_name (tenant_id, parent_id, name),
    root_name VARCHAR(200) GENERATED ALWAYS AS (IF(parent_id IS NULL, name, NULL)) STORED,
    UNIQUE KEY idx_asset_categories_unique_root_name (tenant_id, root_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS ratings (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `tenant_id` bigint NOT NULL,
    `asset_id` bigint NOT NULL,
    `user_id` bigint NOT NULL,
    `score` DECIMAL(20,10) NOT NULL,
    `comment` varchar(2000),
    `tags` json DEFAULT ('[]'),
    `is_handled` boolean DEFAULT false,
    `created_at` datetime(6),
    `updated_at` datetime(6),
    PRIMARY KEY (id),
    KEY idx_asset_ratings_tenant_id (tenant_id),
    UNIQUE KEY uidx_rating_user_asset (asset_id, user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS type_definitions (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `tenant_id` bigint NOT NULL,
    `name` varchar(100) NOT NULL,
    `code` varchar(50) NOT NULL,
    `icon_url` varchar(500),
    `description` varchar(500),
    `enabled` boolean DEFAULT true,
    `sort_order` bigint DEFAULT 0,
    `created_at` datetime(6),
    `updated_at` datetime(6),
    PRIMARY KEY (id),
    KEY idx_asset_type_definitions_tenant_id (tenant_id),
    UNIQUE KEY uidx_type_code_tenant (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS type_field_schemas (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `type_id` bigint NOT NULL,
    `field_key` varchar(100) NOT NULL,
    `field_name` varchar(200) NOT NULL,
    `field_type` varchar(50) NOT NULL,
    `required` boolean DEFAULT false,
    `schema` json DEFAULT ('{}'),
    `sort_order` bigint DEFAULT 0,
    `created_at` datetime(6),
    PRIMARY KEY (id),
    KEY idx_asset_type_field_schemas_type_id (type_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
