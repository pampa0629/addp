CREATE TABLE IF NOT EXISTS engine_access_initial_grant_outcomes (
    tenant_id BIGINT NOT NULL,
    producer_owner VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    execution_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    result_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    outcome VARCHAR(6) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    recorded_at DATETIME(6) NOT NULL,
    grant_request_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin,
    denial_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin,
    issuer_principal_id BIGINT NOT NULL,
    issuer_membership_id BIGINT NOT NULL,
    PRIMARY KEY(tenant_id, producer_owner, execution_id, result_id),
    FOREIGN KEY(tenant_id) REFERENCES tenants(id),
    FOREIGN KEY(issuer_principal_id) REFERENCES principals(id),
    FOREIGN KEY(issuer_membership_id) REFERENCES tenant_memberships(id),
    FOREIGN KEY(tenant_id, producer_owner, execution_id, result_id, grant_request_id, issuer_principal_id, issuer_membership_id)
        REFERENCES engine_access_grants(tenant_id, producer_owner, production_execution_id, production_result_id, request_id, issuer_principal_id, issuer_membership_id),
    CHECK (REGEXP_LIKE(producer_owner, '^[a-z][a-z0-9_]*$', 'c')),
    CHECK (REGEXP_LIKE(execution_id, '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$', 'c') AND execution_id <> '00000000-0000-0000-0000-000000000000'),
    CHECK (REGEXP_LIKE(result_id, '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$', 'c') AND result_id <> '00000000-0000-0000-0000-000000000000'),
    CHECK ((outcome='issued' AND grant_request_id IS NOT NULL AND denial_code IS NULL)
        OR (outcome='denied' AND grant_request_id IS NULL AND denial_code IS NOT NULL
            AND denial_code IN ('production_ineligible','source_access_denied','execution_permission_denied','target_history_exists','engine_changed')))
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS engine_access_upload_grant_outcomes (
    tenant_id BIGINT NOT NULL,
    upload_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    item_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    outcome VARCHAR(6) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    recorded_at DATETIME(6) NOT NULL,
    grant_request_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin,
    denial_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin,
    issuer_principal_id BIGINT NOT NULL,
    issuer_membership_id BIGINT NOT NULL,
    PRIMARY KEY(tenant_id, upload_id, item_fingerprint),
    FOREIGN KEY(tenant_id) REFERENCES tenants(id),
    FOREIGN KEY(issuer_principal_id) REFERENCES principals(id),
    FOREIGN KEY(issuer_membership_id) REFERENCES tenant_memberships(id),
    FOREIGN KEY(tenant_id, upload_id, item_fingerprint, grant_request_id, issuer_principal_id, issuer_membership_id)
        REFERENCES engine_access_grants(tenant_id, upload_id, item_fingerprint, request_id, issuer_principal_id, issuer_membership_id),
    CHECK (REGEXP_LIKE(upload_id, '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$', 'c') AND upload_id <> '00000000-0000-0000-0000-000000000000'),
    CHECK (REGEXP_LIKE(item_fingerprint, '^[0-9a-f]{64}$', 'c')),
    CHECK ((outcome='issued' AND grant_request_id IS NOT NULL AND denial_code IS NULL)
        OR (outcome='denied' AND grant_request_id IS NULL AND denial_code IS NOT NULL
            AND denial_code IN ('not_new','boundary_ineligible','target_history_exists','uploader_inactive','engine_changed')))
) ENGINE=InnoDB;
