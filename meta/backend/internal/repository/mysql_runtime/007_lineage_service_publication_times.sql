CREATE TABLE IF NOT EXISTS meta.lineage_service_publication_times (
    tenant_id BIGINT NOT NULL,
    service_id BIGINT NOT NULL,
    service_updated_at DATETIME(6) NOT NULL,
    PRIMARY KEY (tenant_id, service_id)
) ENGINE=InnoDB
