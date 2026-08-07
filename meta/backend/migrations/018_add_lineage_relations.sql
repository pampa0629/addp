CREATE TABLE IF NOT EXISTS meta.lineage_item_relations (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    source_item_id BIGINT NOT NULL REFERENCES meta.meta_item(id),
    target_item_id BIGINT NOT NULL REFERENCES meta.meta_item(id),
    relation_kind VARCHAR(32) NOT NULL CHECK (relation_kind IN ('derive', 'reference')),
    granularity VARCHAR(32) NOT NULL DEFAULT 'item' CHECK (granularity IN ('item', 'field')),
    write_mode VARCHAR(32),
    status VARCHAR(32) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'unverified', 'stale', 'closed')),
    first_observed_at TIMESTAMPTZ NOT NULL,
    last_observed_at TIMESTAMPTZ NOT NULL,
    closed_at TIMESTAMPTZ,
    closed_by_observation_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT lineage_item_relations_distinct_items CHECK (source_item_id <> target_item_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_lineage_item_relations_active
    ON meta.lineage_item_relations (tenant_id, source_item_id, target_item_id, relation_kind, granularity)
    WHERE status <> 'closed';
CREATE INDEX IF NOT EXISTS idx_lineage_item_relations_source
    ON meta.lineage_item_relations (tenant_id, source_item_id, status);
CREATE INDEX IF NOT EXISTS idx_lineage_item_relations_target
    ON meta.lineage_item_relations (tenant_id, target_item_id, status);

CREATE TABLE IF NOT EXISTS meta.lineage_service_dependencies (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    source_item_id BIGINT NOT NULL REFERENCES meta.meta_item(id),
    service_id BIGINT NOT NULL,
    published_revision VARCHAR(128) NOT NULL,
    dependency_hash VARCHAR(128),
    dependency_kind VARCHAR(32) NOT NULL,
    granularity VARCHAR(32) NOT NULL DEFAULT 'item' CHECK (granularity IN ('item', 'field')),
    dependency_fields JSONB,
    status VARCHAR(32) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'unverified', 'stale', 'closed')),
    first_observed_at TIMESTAMPTZ NOT NULL,
    last_observed_at TIMESTAMPTZ NOT NULL,
    closed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_lineage_service_dependencies_active
    ON meta.lineage_service_dependencies (tenant_id, source_item_id, service_id, published_revision, granularity)
    WHERE status <> 'closed';
CREATE INDEX IF NOT EXISTS idx_lineage_service_dependencies_service
    ON meta.lineage_service_dependencies (tenant_id, service_id, published_revision, status);

CREATE TABLE IF NOT EXISTS meta.lineage_observations (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    relation_kind VARCHAR(32) NOT NULL CHECK (relation_kind IN ('derive', 'reference', 'serve')),
    granularity VARCHAR(32) NOT NULL DEFAULT 'item' CHECK (granularity IN ('item', 'field')),
    source_item_id BIGINT REFERENCES meta.meta_item(id),
    target_item_id BIGINT REFERENCES meta.meta_item(id),
    service_id BIGINT,
    published_revision VARCHAR(128),
    execution_id VARCHAR(64),
    producer_module VARCHAR(64) NOT NULL,
    capture_method VARCHAR(32) NOT NULL CHECK (capture_method IN ('declared', 'runtime', 'parsed')),
    source_snapshot JSONB NOT NULL,
    target_snapshot JSONB,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    observed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT lineage_observations_endpoint_shape CHECK (
        (relation_kind IN ('derive', 'reference') AND source_item_id IS NOT NULL AND target_item_id IS NOT NULL)
        OR (relation_kind = 'serve' AND source_item_id IS NOT NULL AND service_id IS NOT NULL AND published_revision IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_lineage_observations_source
    ON meta.lineage_observations (tenant_id, source_item_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_lineage_observations_target
    ON meta.lineage_observations (tenant_id, target_item_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_lineage_observations_execution
    ON meta.lineage_observations (tenant_id, execution_id);
CREATE INDEX IF NOT EXISTS idx_lineage_observations_service
    ON meta.lineage_observations (tenant_id, service_id, published_revision);
