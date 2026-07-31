CREATE TABLE IF NOT EXISTS manager.data_profiles (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    item_fingerprint VARCHAR(64) NOT NULL,
    item_id BIGINT,
    engine_id BIGINT NOT NULL,
    locator TEXT NOT NULL,
    source_version VARCHAR(64) NOT NULL,
    dependency_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    profile_mode VARCHAR(32) NOT NULL,
    profile_config_hash VARCHAR(64) NOT NULL,
    data_scope JSONB NOT NULL DEFAULT '{"kind":"all"}'::jsonb,
    schema_version VARCHAR(64) NOT NULL,
    sample_method VARCHAR(64) NOT NULL,
    sample_size BIGINT NOT NULL,
    rows_scanned BIGINT NOT NULL,
    row_count BIGINT,
    row_count_exact BOOLEAN NOT NULL,
    field_count INTEGER NOT NULL,
    truncated BOOLEAN NOT NULL,
    partial BOOLEAN NOT NULL,
    observations JSONB NOT NULL DEFAULT '[]'::jsonb,
    last_execution_id VARCHAR(36) NOT NULL,
    profiled_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_data_profiles_current
    ON manager.data_profiles (tenant_id, item_fingerprint, profile_mode, profile_config_hash);
CREATE INDEX IF NOT EXISTS idx_data_profiles_tenant_item
    ON manager.data_profiles (tenant_id, item_fingerprint, item_id);
CREATE INDEX IF NOT EXISTS idx_data_profiles_last_execution_id
    ON manager.data_profiles (last_execution_id);

CREATE TABLE IF NOT EXISTS manager.data_profile_fields (
    id BIGSERIAL PRIMARY KEY,
    profile_id BIGINT NOT NULL REFERENCES manager.data_profiles(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    name VARCHAR(512) NOT NULL,
    type VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    profile JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_data_profile_fields_profile_name
    ON manager.data_profile_fields (profile_id, name);
CREATE INDEX IF NOT EXISTS idx_data_profile_fields_profile_position
    ON manager.data_profile_fields (profile_id, position);
