CREATE TABLE IF NOT EXISTS context_selection_ticket_options (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    ticket_id BIGINT NOT NULL,
    context_type VARCHAR(16) NOT NULL,
    tenant_membership_id BIGINT,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    platform_ticket_id BIGINT GENERATED ALWAYS AS (CASE WHEN context_type='platform' THEN ticket_id END) STORED UNIQUE,
    UNIQUE KEY uq_selection_tenant_option (ticket_id, tenant_membership_id),
    FOREIGN KEY (ticket_id) REFERENCES context_selection_tickets (id),
    FOREIGN KEY (tenant_membership_id) REFERENCES tenant_memberships (id),
    CHECK ((context_type='platform' AND tenant_membership_id IS NULL) OR (context_type='tenant' AND tenant_membership_id IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
