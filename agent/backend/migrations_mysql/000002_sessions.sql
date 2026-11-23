CREATE TABLE IF NOT EXISTS agent.sessions (
    id INTEGER NOT NULL AUTO_INCREMENT,
    user_id INTEGER NOT NULL,
    tenant_id INTEGER NOT NULL,
    title VARCHAR(255),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    pinned BOOL NOT NULL DEFAULT false,
    archived BOOL NOT NULL DEFAULT false,
    summary LONGTEXT,
    summary_message_id INTEGER,
    created_at DATETIME(6) DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_agent_sessions_organization (tenant_id,user_id,archived,pinned DESC,updated_at DESC,id DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
