CREATE TABLE IF NOT EXISTS engine_access_fulfillment_outcomes (
    request_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    engine_id BIGINT NOT NULL,
    caller_principal_id BIGINT NOT NULL,
    catalog_path JSON NOT NULL,
    binding JSON NOT NULL,
    expiry_mode VARCHAR(24) NOT NULL,
    grant_expires_at DATETIME(6),
    outcome VARCHAR(16) NOT NULL,
    recorded_at DATETIME(6) NOT NULL,
    deadline DATETIME(6),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (tenant_id,engine_id) REFERENCES engines(tenant_id,id),
    FOREIGN KEY (caller_principal_id) REFERENCES service_principals(id),
    CHECK (REGEXP_LIKE(request_id,'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$','c')
        AND request_id <> '00000000-0000-0000-0000-000000000000'),
    CHECK ((JSON_TYPE(catalog_path)='OBJECT' AND JSON_TYPE(binding)='OBJECT'
        AND catalog_path->>'$.engine_id'=CAST(engine_id AS CHAR)
        AND REGEXP_LIKE(binding->>'$.item_fingerprint','^[0-9a-f]{64}$','c')
        AND REGEXP_LIKE(binding->>'$.boundary_hash','^[0-9a-f]{64}$','c')) IS TRUE),
    CHECK ((binding->>'$.expiry_mode'=expiry_mode AND
        ((expiry_mode='until_revoked' AND grant_expires_at IS NULL AND JSON_TYPE(binding->'$.expires_at')='NULL')
        OR (expiry_mode='at_time' AND grant_expires_at IS NOT NULL AND JSON_TYPE(binding->'$.expires_at')='STRING'))) IS TRUE),
    CHECK ((outcome='closed' AND deadline IS NULL) OR
        (outcome='accepted' AND deadline IS NOT NULL AND deadline>recorded_at AND
            deadline=LEAST(recorded_at + INTERVAL 5 MINUTE, COALESCE(grant_expires_at,recorded_at + INTERVAL 5 MINUTE))))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
