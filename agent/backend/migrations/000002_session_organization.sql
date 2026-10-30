ALTER TABLE agent.sessions
 ADD COLUMN version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
 ADD COLUMN pinned BOOLEAN NOT NULL DEFAULT false,
 ADD COLUMN archived BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX idx_agent_sessions_organization ON agent.sessions (tenant_id, user_id, archived, pinned DESC, updated_at DESC, id DESC);
