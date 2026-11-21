CREATE DEFINER='addp_system_source_grant'@'localhost' PROCEDURE system.persist_source_grant(
    IN p_approval_mode VARCHAR(32), IN p_request_id CHAR(36),
    IN p_tenant_id BIGINT, IN p_engine_id BIGINT, IN p_catalog_path JSON,
    IN p_catalog_request_id CHAR(36), IN p_node_recursive BOOLEAN,
    IN p_item_fingerprint VARCHAR(64), IN p_boundary_hash VARCHAR(64),
    IN p_file_composition JSON, IN p_scope_boundary JSON, IN p_metadata_target JSON,
    IN p_recipient_type VARCHAR(32), IN p_recipient_id BIGINT, IN p_action VARCHAR(32),
    IN p_expiry_mode VARCHAR(32), IN p_expires_at DATETIME(6),
    IN p_operator_principal_id BIGINT, IN p_operator_membership_id BIGINT,
    IN p_operator_authorization_version BIGINT, IN p_reason LONGTEXT,
    IN p_producer_owner VARCHAR(255), IN p_execution_id CHAR(36), IN p_result_id CHAR(36),
    IN p_upload_id CHAR(36), IN p_issuer_principal_id BIGINT, IN p_issuer_membership_id BIGINT,
    IN p_recorded_at DATETIME(6), IN p_denial_code VARCHAR(64)
)
SQL SECURITY DEFINER
MODIFIES SQL DATA
BEGIN
    -- In autocommit mode the savepoint is gone at the next statement.
    SAVEPOINT addp_system_source_pair;
    ROLLBACK TO SAVEPOINT addp_system_source_pair;
    BEGIN
        DECLARE EXIT HANDLER FOR SQLEXCEPTION
        BEGIN
            ROLLBACK TO SAVEPOINT addp_system_source_pair;
            RELEASE SAVEPOINT addp_system_source_pair;
            RESIGNAL;
        END;

        IF p_approval_mode IS NULL OR BINARY p_approval_mode NOT IN
            ('catalog', 'independent', 'task_production', 'upload_creation') THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='invalid source grant origin';
        END IF;
        IF p_approval_mode IN ('task_production', 'upload_creation') THEN
            IF (p_request_id IS NULL) = (p_denial_code IS NULL) OR p_recorded_at IS NULL
                OR p_issuer_principal_id IS NULL OR p_issuer_membership_id IS NULL
                OR (p_approval_mode='task_production' AND
                    (p_producer_owner IS NULL OR p_execution_id IS NULL OR p_result_id IS NULL OR p_upload_id IS NOT NULL))
                OR (p_approval_mode='upload_creation' AND
                    (p_upload_id IS NULL OR p_item_fingerprint IS NULL OR p_producer_owner IS NOT NULL
                     OR p_execution_id IS NOT NULL OR p_result_id IS NOT NULL)) THEN
                SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='invalid initial source grant result';
            END IF;
        ELSEIF p_request_id IS NULL OR p_denial_code IS NOT NULL OR p_recorded_at IS NOT NULL
            OR p_producer_owner IS NOT NULL OR p_execution_id IS NOT NULL OR p_result_id IS NOT NULL
            OR p_upload_id IS NOT NULL OR p_issuer_principal_id IS NOT NULL OR p_issuer_membership_id IS NOT NULL THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='invalid ordinary source grant';
        END IF;

        IF p_request_id IS NOT NULL THEN
            INSERT INTO system.engine_access_grants (
                request_id, approval_mode, tenant_id, engine_id, catalog_path, catalog_request_id,
                node_recursive, item_fingerprint, boundary_hash, file_composition, scope_boundary,
                metadata_target, recipient_type, recipient_id, action, expiry_mode, expires_at,
                operator_principal_id, operator_membership_id, operator_authorization_version,
                reason, producer_owner, production_execution_id, production_result_id, upload_id,
                issuer_principal_id, issuer_membership_id
            ) VALUES (
                p_request_id, p_approval_mode, p_tenant_id, p_engine_id, p_catalog_path, p_catalog_request_id,
                p_node_recursive, p_item_fingerprint, p_boundary_hash, p_file_composition, p_scope_boundary,
                p_metadata_target, p_recipient_type, p_recipient_id, p_action, p_expiry_mode, p_expires_at,
                p_operator_principal_id, p_operator_membership_id, p_operator_authorization_version,
                p_reason, p_producer_owner, p_execution_id, p_result_id, p_upload_id,
                p_issuer_principal_id, p_issuer_membership_id
            );
        END IF;

        IF p_approval_mode='task_production' THEN
            INSERT INTO system.engine_access_initial_grant_outcomes (
                tenant_id, producer_owner, execution_id, result_id, outcome, recorded_at,
                grant_request_id, denial_code, issuer_principal_id, issuer_membership_id
            ) VALUES (
                p_tenant_id, p_producer_owner, p_execution_id, p_result_id,
                IF(p_request_id IS NULL,'denied','issued'), p_recorded_at,
                p_request_id, p_denial_code, p_issuer_principal_id, p_issuer_membership_id
            );
        ELSEIF p_approval_mode='upload_creation' THEN
            INSERT INTO system.engine_access_upload_grant_outcomes (
                tenant_id, upload_id, item_fingerprint, outcome, recorded_at, grant_request_id,
                denial_code, issuer_principal_id, issuer_membership_id
            ) VALUES (
                p_tenant_id, p_upload_id, p_item_fingerprint,
                IF(p_request_id IS NULL,'denied','issued'), p_recorded_at, p_request_id,
                p_denial_code, p_issuer_principal_id, p_issuer_membership_id
            );
        END IF;
    END;
    RELEASE SAVEPOINT addp_system_source_pair;
END
