CREATE TABLE IF NOT EXISTS manager.data_profiles (
    id bigint unsigned AUTO_INCREMENT PRIMARY KEY,
    tenant_id bigint unsigned NOT NULL, item_fingerprint varchar(64) NOT NULL,
    item_id bigint unsigned, engine_id bigint unsigned NOT NULL,
    locator text NOT NULL, source_version varchar(64) NOT NULL,
    dependency_snapshot json NOT NULL DEFAULT (JSON_OBJECT()),
    profile_mode varchar(32) NOT NULL, profile_config_hash varchar(64) NOT NULL,
    data_scope json NOT NULL DEFAULT (JSON_OBJECT('kind','all')),
    schema_version varchar(64) NOT NULL, sample_method varchar(64) NOT NULL,
    sample_size bigint NOT NULL, rows_scanned bigint NOT NULL, row_count bigint,
    row_count_exact boolean NOT NULL, field_count bigint NOT NULL,
    truncated boolean NOT NULL, partial boolean NOT NULL,
    observations json NOT NULL DEFAULT (JSON_ARRAY()), last_execution_id varchar(36) NOT NULL,
    profiled_at datetime(6) NOT NULL, created_at datetime(6) NOT NULL, updated_at datetime(6) NOT NULL,
    UNIQUE KEY idx_data_profiles_current (tenant_id,item_fingerprint,profile_mode,profile_config_hash),
    KEY idx_data_profiles_tenant_item (item_fingerprint,item_id),
    KEY idx_data_profiles_execution (last_execution_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS manager.data_profile_fields (
    id bigint unsigned AUTO_INCREMENT PRIMARY KEY, profile_id bigint unsigned NOT NULL,
    position bigint NOT NULL, name varchar(512) NOT NULL, type varchar(64) NOT NULL,
    status varchar(32) NOT NULL, profile json NOT NULL, created_at datetime(6) NOT NULL, updated_at datetime(6) NOT NULL,
    UNIQUE KEY idx_data_profile_fields_profile_name (profile_id,name),
    KEY idx_data_profile_fields_profile_position (profile_id,position),
    FOREIGN KEY (profile_id) REFERENCES manager.data_profiles(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

-- Serialize admission before looking for the first execution of a tenant target.
CREATE TABLE IF NOT EXISTS manager.data_profile_target_locks (
    lock_id bigint NOT NULL PRIMARY KEY
) ENGINE=InnoDB;
