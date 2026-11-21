CREATE TABLE IF NOT EXISTS engine_access_grants (
    request_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    granted_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    approval_mode VARCHAR(32) NOT NULL,
    catalog_request_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin,
    tenant_id BIGINT NOT NULL,
    engine_id BIGINT NOT NULL,
    catalog_path JSON NOT NULL,
    node_recursive BOOLEAN,
    item_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    boundary_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    file_composition JSON,
    scope_boundary JSON,
    metadata_target JSON,
    recipient_type VARCHAR(32) NOT NULL,
    recipient_id BIGINT NOT NULL,
    action VARCHAR(32) NOT NULL,
    expiry_mode VARCHAR(32) NOT NULL,
    expires_at DATETIME(6),
    operator_principal_id BIGINT NOT NULL,
    operator_membership_id BIGINT NOT NULL,
    operator_authorization_version BIGINT NOT NULL,
    reason LONGTEXT,
    producer_owner VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin,
    production_execution_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin,
    production_result_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin,
    upload_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin,
    issuer_principal_id BIGINT,
    issuer_membership_id BIGINT,
    FOREIGN KEY (catalog_request_id) REFERENCES engine_access_fulfillment_outcomes(request_id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (tenant_id,engine_id) REFERENCES engines(tenant_id,id),
    FOREIGN KEY (operator_principal_id) REFERENCES principals(id),
    FOREIGN KEY (operator_membership_id) REFERENCES tenant_memberships(id),
    FOREIGN KEY (issuer_principal_id) REFERENCES principals(id),
    FOREIGN KEY (issuer_membership_id) REFERENCES tenant_memberships(id),
    UNIQUE (tenant_id,producer_owner,production_execution_id,production_result_id),
    UNIQUE (tenant_id,producer_owner,production_execution_id,production_result_id,request_id,issuer_principal_id,issuer_membership_id),
    UNIQUE (tenant_id,upload_id,item_fingerprint,request_id,issuer_principal_id,issuer_membership_id),
    KEY idx_source_grant_item (tenant_id,engine_id,item_fingerprint,boundary_hash),
    CHECK (REGEXP_LIKE(request_id,'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$','c')
        AND request_id <> '00000000-0000-0000-0000-000000000000'),
    CHECK ((JSON_TYPE(catalog_path)='OBJECT' AND catalog_path->>'$.engine_id'=CAST(engine_id AS CHAR)
        AND catalog_path->>'$.version'='catalog.path/v1' AND JSON_TYPE(catalog_path->'$.segments')='ARRAY') IS TRUE),
    CHECK (recipient_type IN ('user','department','project_group') AND recipient_id>0),
    CHECK (action IN ('read','metadata_read') AND operator_authorization_version>0),
    CHECK ((expiry_mode='until_revoked' AND expires_at IS NULL)
        OR (expiry_mode='at_time' AND expires_at IS NOT NULL AND expires_at>granted_at)),
    CHECK ((approval_mode='catalog' AND catalog_request_id IS NOT NULL AND catalog_request_id=request_id AND reason IS NULL)
        OR (approval_mode='independent' AND catalog_request_id IS NULL AND reason IS NOT NULL
            AND reason=TRIM(reason) AND CHAR_LENGTH(reason) BETWEEN 1 AND 2000)
        OR (approval_mode IN ('task_production','upload_creation') AND catalog_request_id IS NULL AND reason IS NULL)),
    CHECK ((CASE approval_mode
        WHEN 'task_production' THEN producer_owner IS NOT NULL AND REGEXP_LIKE(producer_owner,'^[a-z][a-z0-9_]*$','c')
            AND production_execution_id IS NOT NULL AND production_result_id IS NOT NULL AND upload_id IS NULL
            AND issuer_principal_id IS NOT NULL AND issuer_membership_id IS NOT NULL
            AND recipient_type='user' AND recipient_id=operator_principal_id
        WHEN 'upload_creation' THEN producer_owner IS NULL AND production_execution_id IS NULL AND production_result_id IS NULL
            AND upload_id IS NOT NULL AND issuer_principal_id IS NOT NULL AND issuer_membership_id IS NOT NULL
            AND recipient_type='user' AND recipient_id=operator_principal_id AND expiry_mode='until_revoked'
            AND node_recursive IS NULL AND scope_boundary IS NULL
        ELSE producer_owner IS NULL AND production_execution_id IS NULL AND production_result_id IS NULL AND upload_id IS NULL
            AND issuer_principal_id IS NULL AND issuer_membership_id IS NULL END) IS TRUE),
    CHECK ((node_recursive IS NULL AND REGEXP_LIKE(item_fingerprint,'^[0-9a-f]{64}$','c')
            AND REGEXP_LIKE(boundary_hash,'^[0-9a-f]{64}$','c'))
        OR (node_recursive IN (0,1) AND node_recursive IS NOT NULL AND approval_mode='independent'
            AND item_fingerprint='' AND boundary_hash='' AND file_composition IS NULL AND scope_boundary IS NULL AND metadata_target IS NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
