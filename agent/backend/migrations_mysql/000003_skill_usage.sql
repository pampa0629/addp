CREATE TABLE IF NOT EXISTS agent.skill_usage (
    id INTEGER NOT NULL AUTO_INCREMENT,
    skill_name VARCHAR(255) NOT NULL,
    user_id INTEGER NOT NULL,
    tenant_id INTEGER NOT NULL,
    success BOOL,
    execution_time_ms INTEGER,
    created_at DATETIME(6) DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
