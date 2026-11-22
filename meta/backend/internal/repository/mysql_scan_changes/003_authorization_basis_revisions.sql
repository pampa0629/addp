CREATE TABLE IF NOT EXISTS meta.authorization_basis_revisions (
    tenant_id BIGINT NOT NULL PRIMARY KEY CHECK (tenant_id>0),
    version BIGINT NOT NULL CHECK (version>0)
) ENGINE=InnoDB
