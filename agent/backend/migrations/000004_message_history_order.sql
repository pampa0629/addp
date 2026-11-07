CREATE INDEX ix_agent_messages_session_order ON agent.messages (session_id, created_at, id);
