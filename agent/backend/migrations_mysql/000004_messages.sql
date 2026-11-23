CREATE TABLE IF NOT EXISTS agent.messages (
    id INTEGER NOT NULL AUTO_INCREMENT,
    session_id INTEGER NOT NULL,
    `role` VARCHAR(50) NOT NULL,
    content LONGTEXT NOT NULL,
    protocol_message_id VARCHAR(100),
    parts JSON NOT NULL,
    is_complete BOOL NOT NULL DEFAULT TRUE,
    created_at DATETIME(6) DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    FOREIGN KEY(session_id) REFERENCES agent.sessions (id) ON DELETE CASCADE,
    UNIQUE KEY idx_agent_messages_session_protocol_id (session_id,protocol_message_id),
    KEY ix_agent_messages_session_order (`session_id`, `created_at`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
