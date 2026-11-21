CREATE TRIGGER IF NOT EXISTS source_grant_provenance
BEFORE INSERT ON engine_access_grants FOR EACH ROW
BEGIN
    DECLARE receipt_binding,receipt_path JSON;
    DECLARE receipt_tenant,receipt_engine BIGINT;
    DECLARE receipt_mode VARCHAR(32);
    DECLARE receipt_expiry DATETIME(6);
    DECLARE received_at, receipt_deadline DATETIME(6);
    DECLARE runtime_owner VARCHAR(255);
    SET NEW.granted_at=CURRENT_TIMESTAMP(6);

    IF NEW.approval_mode='catalog' THEN
        SELECT binding,recorded_at,deadline,tenant_id,engine_id,catalog_path,expiry_mode,grant_expires_at
          INTO receipt_binding,received_at,receipt_deadline,receipt_tenant,receipt_engine,
               receipt_path,receipt_mode,receipt_expiry
          FROM engine_access_fulfillment_outcomes WHERE request_id=NEW.request_id AND outcome='accepted';
        IF receipt_deadline IS NULL OR NEW.granted_at<received_at OR NEW.granted_at>=receipt_deadline THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='source Grant requires an unexpired accepted Catalog receipt';
        END IF;
        SET NEW.catalog_request_id=NEW.request_id, NEW.tenant_id=receipt_tenant, NEW.engine_id=receipt_engine,
            NEW.catalog_path=receipt_path, NEW.expiry_mode=receipt_mode, NEW.expires_at=receipt_expiry,
            NEW.item_fingerprint=JSON_UNQUOTE(JSON_EXTRACT(receipt_binding,'$.item_fingerprint')),
            NEW.boundary_hash=JSON_UNQUOTE(JSON_EXTRACT(receipt_binding,'$.boundary_hash')),
            NEW.recipient_type=JSON_UNQUOTE(JSON_EXTRACT(receipt_binding,'$.recipient_type')),
            NEW.recipient_id=JSON_UNQUOTE(JSON_EXTRACT(receipt_binding,'$.recipient_id')),
            NEW.action=JSON_UNQUOTE(JSON_EXTRACT(receipt_binding,'$.action')),
            NEW.operator_principal_id=JSON_UNQUOTE(JSON_EXTRACT(receipt_binding,'$.operator.principal_id')),
            NEW.operator_membership_id=JSON_UNQUOTE(JSON_EXTRACT(receipt_binding,'$.operator.tenant_membership_id')),
            NEW.operator_authorization_version=JSON_UNQUOTE(JSON_EXTRACT(receipt_binding,'$.operator.authorization_version')),
            NEW.reason=NULL;
    ELSE
        IF NOT EXISTS (
            SELECT 1 FROM principals p JOIN users u ON u.id=p.id
            JOIN tenant_memberships m ON m.principal_id=p.id JOIN tenants t ON t.id=m.tenant_id
            WHERE p.id=NEW.operator_principal_id AND p.principal_type='user' AND p.status='active'
              AND p.authorization_version=NEW.operator_authorization_version AND m.id=NEW.operator_membership_id
              AND m.tenant_id=NEW.tenant_id AND m.status='active' AND m.joined_at<=NEW.granted_at
              AND (m.expires_at IS NULL OR m.expires_at>NEW.granted_at) AND t.status='active'
        ) THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='source Grant requires current user and tenant membership';
        END IF;
        IF NEW.approval_mode='independent' THEN
            IF NOT EXISTS (SELECT 1 FROM engines WHERE id=NEW.engine_id AND tenant_id=NEW.tenant_id AND lifecycle_state='active') THEN
                SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='independent Grant requires active tenant engine';
            END IF;
            IF NOT ((NEW.recipient_type='user' AND EXISTS (
                SELECT 1 FROM principals p JOIN tenant_memberships m ON m.principal_id=p.id
                WHERE p.id=NEW.recipient_id AND p.principal_type='user' AND p.status='active'
                  AND m.tenant_id=NEW.tenant_id AND m.status='active' AND m.joined_at<=NEW.granted_at
                  AND (m.expires_at IS NULL OR m.expires_at>NEW.granted_at)))
                OR (NEW.recipient_type='department' AND EXISTS (
                    SELECT 1 FROM departments WHERE id=NEW.recipient_id AND tenant_id=NEW.tenant_id AND status='active'))
                OR (NEW.recipient_type='project_group' AND EXISTS (
                    SELECT 1 FROM project_groups WHERE id=NEW.recipient_id AND tenant_id=NEW.tenant_id AND status='active'))) THEN
                SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='independent Grant requires current tenant recipient';
            END IF;
        ELSEIF NEW.approval_mode IN ('task_production','upload_creation') THEN
            SET runtime_owner=IF(NEW.approval_mode='upload_creation','manager',NEW.producer_owner);
            IF NOT EXISTS (
                SELECT 1 FROM principals p JOIN service_principals s ON s.id=p.id
                JOIN tenant_memberships m ON m.principal_id=p.id JOIN oauth_clients c ON c.service_principal_id=p.id
                WHERE p.id=NEW.issuer_principal_id AND p.principal_type='service_principal' AND p.status='active'
                  AND s.name=CONCAT('addp-',runtime_owner) AND s.owner_scope='platform'
                  AND c.client_id=CONCAT('addp-',runtime_owner) AND c.status='active'
                  AND m.id=NEW.issuer_membership_id AND m.tenant_id=NEW.tenant_id AND m.status='active'
                  AND m.joined_at<=NEW.granted_at AND (m.expires_at IS NULL OR m.expires_at>NEW.granted_at)
            ) THEN
                SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='initial Grant requires current source runtime in tenant';
            END IF;
            IF NEW.approval_mode='task_production' AND NOT EXISTS (
                SELECT 1 FROM permissions WHERE permission_key=CONCAT(runtime_owner,'.production_receipt.read')
                  AND owner_module=runtime_owner AND status='active' AND NOT delegable AND NOT tenant_customizable
            ) THEN
                SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='production Grant requires registered receipt permission';
            END IF;
            IF NEW.approval_mode='upload_creation' AND NOT EXISTS (
                SELECT 1 FROM engines WHERE id=NEW.engine_id AND tenant_id=NEW.tenant_id
                AND engine_type='nfs' AND lifecycle_state='active'
            ) THEN
                SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='upload Grant requires active tenant NFS engine';
            END IF;
        END IF;
    END IF;
    IF JSON_UNQUOTE(JSON_EXTRACT(NEW.file_composition,'$.format'))='geopackage' AND NOT EXISTS (
        SELECT 1 FROM engines WHERE tenant_id=NEW.tenant_id AND id=NEW.engine_id AND engine_type='nfs' AND lifecycle_state='active'
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='source_grant_geopackage_nfs';
    END IF;
END
