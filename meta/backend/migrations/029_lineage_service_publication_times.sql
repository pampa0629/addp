CREATE TABLE meta.lineage_service_publication_times (
    tenant_id BIGINT NOT NULL,
    service_id BIGINT NOT NULL,
    service_updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, service_id)
);

INSERT INTO meta.lineage_service_publication_times (tenant_id, service_id, service_updated_at)
SELECT tenant_id, service_id, MAX(service_updated_at)
FROM meta.lineage_service_dependencies
WHERE service_updated_at IS NOT NULL
GROUP BY tenant_id, service_id;
