CREATE TABLE IF NOT EXISTS agent.run_events (
    id CHAR(32) NOT NULL,
    agent_run_id CHAR(32) NOT NULL,
    sequence INTEGER NOT NULL,
    protocol_invocation_id VARCHAR(100) NOT NULL,
    event_type VARCHAR(80) NOT NULL,
    payload JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
    PRIMARY KEY (id),
    CONSTRAINT uq_agent_run_events_sequence UNIQUE (agent_run_id, sequence),
    FOREIGN KEY(agent_run_id) REFERENCES agent.runs (id) ON DELETE CASCADE,
    KEY ix_agent_run_events_agent_run_id (`agent_run_id`),
    KEY ix_agent_run_events_event_type (`event_type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
