CREATE TABLE IF NOT EXISTS search_authorization_revisions (
    tenant_id BIGINT PRIMARY KEY CHECK (tenant_id > 0),
    version BIGINT NOT NULL CHECK (version > 0)
) ENGINE=InnoDB
