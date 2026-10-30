-- Agent-owned initial schema and adoption of the pre-versioned schema.


CREATE TABLE IF NOT EXISTS agent.inference_scenario_bindings (
	id BIGSERIAL NOT NULL,
	scenario_code VARCHAR(80) NOT NULL,
	scope_type VARCHAR(16) NOT NULL,
	tenant_id BIGINT,
	model_profile_id UUID NOT NULL,
	version BIGINT NOT NULL,
	updated_by BIGINT NOT NULL,
	created_at TIMESTAMP WITH TIME ZONE DEFAULT now() NOT NULL,
	updated_at TIMESTAMP WITH TIME ZONE DEFAULT now() NOT NULL,
	PRIMARY KEY (id),
	CONSTRAINT ck_agent_inference_binding_tenant CHECK ((scope_type = 'platform' AND tenant_id IS NULL) OR (scope_type = 'tenant' AND tenant_id IS NOT NULL)),
	CONSTRAINT ck_agent_inference_binding_scope CHECK (scope_type IN ('platform', 'tenant'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_inference_binding_platform ON agent.inference_scenario_bindings (scenario_code) WHERE scope_type = 'platform' AND tenant_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_inference_binding_tenant ON agent.inference_scenario_bindings (scenario_code, tenant_id) WHERE scope_type = 'tenant' AND tenant_id IS NOT NULL;


CREATE TABLE IF NOT EXISTS agent.sessions (
	id SERIAL NOT NULL,
	user_id INTEGER NOT NULL,
	tenant_id INTEGER NOT NULL,
	title VARCHAR(255),
	summary TEXT,
	summary_message_id INTEGER,
	created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT now(),
	updated_at TIMESTAMP WITHOUT TIME ZONE DEFAULT now(),
	PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS ix_agent_sessions_id ON agent.sessions (id);


CREATE TABLE IF NOT EXISTS agent.skill_usage (
	id SERIAL NOT NULL,
	skill_name VARCHAR(255) NOT NULL,
	user_id INTEGER NOT NULL,
	tenant_id INTEGER NOT NULL,
	success BOOLEAN,
	execution_time_ms INTEGER,
	created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT now(),
	PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS ix_agent_skill_usage_id ON agent.skill_usage (id);


CREATE TABLE IF NOT EXISTS agent.messages (
	id SERIAL NOT NULL,
	session_id INTEGER NOT NULL,
	role VARCHAR(50) NOT NULL,
	content TEXT NOT NULL,
	protocol_message_id VARCHAR(100),
	parts JSONB NOT NULL,
	created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT now(),
	PRIMARY KEY (id),
	FOREIGN KEY(session_id) REFERENCES agent.sessions (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS ix_agent_messages_id ON agent.messages (id);


CREATE TABLE IF NOT EXISTS agent.runs (
	id UUID NOT NULL,
	session_id INTEGER NOT NULL,
	user_id INTEGER NOT NULL,
	tenant_id INTEGER NOT NULL,
	initial_protocol_run_id VARCHAR(100) NOT NULL,
	status VARCHAR(30) NOT NULL,
	skill_name VARCHAR(255),
	checkpoint JSONB NOT NULL,
	metrics JSONB NOT NULL,
	context_metrics JSONB NOT NULL,
	error_source VARCHAR(30),
	error_code VARCHAR(100),
	error_message TEXT,
	created_at TIMESTAMP WITH TIME ZONE DEFAULT now() NOT NULL,
	updated_at TIMESTAMP WITH TIME ZONE DEFAULT now() NOT NULL,
	completed_at TIMESTAMP WITH TIME ZONE,
	PRIMARY KEY (id),
	FOREIGN KEY(session_id) REFERENCES agent.sessions (id) ON DELETE CASCADE,
	CONSTRAINT uq_agent_runs_session_protocol_run UNIQUE (session_id, initial_protocol_run_id)
);

CREATE INDEX IF NOT EXISTS ix_agent_runs_session_id ON agent.runs (session_id);

CREATE INDEX IF NOT EXISTS ix_agent_runs_status ON agent.runs (status);

CREATE INDEX IF NOT EXISTS ix_agent_runs_tenant_id ON agent.runs (tenant_id);

CREATE INDEX IF NOT EXISTS ix_agent_runs_user_id ON agent.runs (user_id);


CREATE TABLE IF NOT EXISTS agent.interactions (
	id UUID NOT NULL,
	session_id INTEGER NOT NULL,
	user_id INTEGER NOT NULL,
	tenant_id INTEGER NOT NULL,
	agent_run_id UUID NOT NULL,
	tool_call_id VARCHAR(100),
	owner_interaction_id VARCHAR(100),
	owner_request_fingerprint VARCHAR(64),
	open_url TEXT,
	request_summary JSONB NOT NULL,
	kind VARCHAR(50) NOT NULL,
	owner VARCHAR(50) NOT NULL,
	status VARCHAR(30) NOT NULL,
	prompt TEXT NOT NULL,
	response_schema JSONB NOT NULL,
	options JSONB NOT NULL,
	answer JSONB,
	created_at TIMESTAMP WITH TIME ZONE DEFAULT now() NOT NULL,
	completed_at TIMESTAMP WITH TIME ZONE,
	expires_at TIMESTAMP WITH TIME ZONE,
	PRIMARY KEY (id),
	FOREIGN KEY(agent_run_id) REFERENCES agent.runs (id) ON DELETE CASCADE,
	FOREIGN KEY(session_id) REFERENCES agent.sessions (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS ix_agent_interactions_agent_run_id ON agent.interactions (agent_run_id);

CREATE INDEX IF NOT EXISTS ix_agent_interactions_owner_interaction_id ON agent.interactions (owner_interaction_id);

CREATE INDEX IF NOT EXISTS ix_agent_interactions_session_id ON agent.interactions (session_id);

CREATE INDEX IF NOT EXISTS ix_agent_interactions_status ON agent.interactions (status);

CREATE INDEX IF NOT EXISTS ix_agent_interactions_tenant_id ON agent.interactions (tenant_id);

CREATE INDEX IF NOT EXISTS ix_agent_interactions_user_id ON agent.interactions (user_id);


CREATE TABLE IF NOT EXISTS agent.run_events (
	id UUID NOT NULL,
	agent_run_id UUID NOT NULL,
	sequence INTEGER NOT NULL,
	protocol_invocation_id VARCHAR(100) NOT NULL,
	event_type VARCHAR(80) NOT NULL,
	payload JSONB NOT NULL,
	created_at TIMESTAMP WITH TIME ZONE DEFAULT now() NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY(agent_run_id) REFERENCES agent.runs (id) ON DELETE CASCADE,
	CONSTRAINT uq_agent_run_events_sequence UNIQUE (agent_run_id, sequence)
);

CREATE INDEX IF NOT EXISTS ix_agent_run_events_agent_run_id ON agent.run_events (agent_run_id);

CREATE INDEX IF NOT EXISTS ix_agent_run_events_event_type ON agent.run_events (event_type);


CREATE TABLE IF NOT EXISTS agent.run_steps (
	id UUID NOT NULL,
	agent_run_id UUID NOT NULL,
	sequence INTEGER NOT NULL,
	step_type VARCHAR(30) NOT NULL,
	status VARCHAR(30) NOT NULL,
	protocol_invocation_id VARCHAR(100) NOT NULL,
	tool_call_id VARCHAR(100),
	tool_name VARCHAR(255),
	input JSONB NOT NULL,
	output_summary TEXT,
	facts JSONB NOT NULL,
	error_source VARCHAR(30),
	error_code VARCHAR(100),
	error_message TEXT,
	started_at TIMESTAMP WITH TIME ZONE DEFAULT now() NOT NULL,
	completed_at TIMESTAMP WITH TIME ZONE,
	PRIMARY KEY (id),
	FOREIGN KEY(agent_run_id) REFERENCES agent.runs (id) ON DELETE CASCADE,
	CONSTRAINT uq_agent_run_steps_sequence UNIQUE (agent_run_id, sequence)
);

CREATE INDEX IF NOT EXISTS ix_agent_run_steps_agent_run_id ON agent.run_steps (agent_run_id);

CREATE INDEX IF NOT EXISTS ix_agent_run_steps_status ON agent.run_steps (status);

CREATE INDEX IF NOT EXISTS ix_agent_run_steps_tool_call_id ON agent.run_steps (tool_call_id);

ALTER TABLE agent.sessions ADD COLUMN IF NOT EXISTS summary TEXT;

ALTER TABLE agent.sessions ADD COLUMN IF NOT EXISTS summary_message_id INTEGER;

ALTER TABLE agent.runs ADD COLUMN IF NOT EXISTS metrics JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE agent.runs ADD COLUMN IF NOT EXISTS context_metrics JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE agent.runs ADD COLUMN IF NOT EXISTS error_source VARCHAR(30);

ALTER TABLE agent.runs ADD COLUMN IF NOT EXISTS error_code VARCHAR(100);

ALTER TABLE agent.runs DROP COLUMN IF EXISTS error_type;

ALTER TABLE agent.run_steps ADD COLUMN IF NOT EXISTS error_source VARCHAR(30);

ALTER TABLE agent.run_steps ADD COLUMN IF NOT EXISTS error_code VARCHAR(100);

ALTER TABLE agent.messages ADD COLUMN IF NOT EXISTS protocol_message_id VARCHAR(100);

ALTER TABLE agent.messages ADD COLUMN IF NOT EXISTS parts JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE agent.messages DROP COLUMN IF EXISTS message_type;

ALTER TABLE agent.messages DROP COLUMN IF EXISTS result_type;

ALTER TABLE agent.messages DROP COLUMN IF EXISTS result_data;

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_messages_session_protocol_id ON agent.messages (session_id, protocol_message_id) WHERE protocol_message_id IS NOT NULL;

DO $$
            BEGIN
                IF EXISTS (
                    SELECT 1
                    FROM information_schema.columns
                    WHERE table_schema = 'agent'
                      AND table_name = 'interactions'
                      AND column_name = 'run_id'
                ) THEN
                    DELETE FROM agent.interactions;
                    ALTER TABLE agent.interactions DROP COLUMN run_id;
                END IF;
            END
            $$;

ALTER TABLE agent.interactions ADD COLUMN IF NOT EXISTS agent_run_id UUID;

DELETE FROM agent.interactions WHERE agent_run_id IS NULL;

ALTER TABLE agent.interactions ALTER COLUMN agent_run_id SET NOT NULL;

DO $$
            BEGIN
                IF NOT EXISTS (
                    SELECT 1
                    FROM pg_constraint
                    WHERE conrelid = 'agent.interactions'::regclass
                      AND confrelid = 'agent.runs'::regclass
                      AND contype = 'f'
                ) THEN
                    ALTER TABLE agent.interactions
                    ADD CONSTRAINT fk_agent_interactions_run
                    FOREIGN KEY (agent_run_id) REFERENCES agent.runs(id) ON DELETE CASCADE;
                END IF;
            END
            $$;

CREATE INDEX IF NOT EXISTS idx_agent_interactions_agent_run_id ON agent.interactions (agent_run_id);

ALTER TABLE agent.interactions ADD COLUMN IF NOT EXISTS owner_interaction_id VARCHAR(100);

ALTER TABLE agent.interactions ADD COLUMN IF NOT EXISTS owner_request_fingerprint VARCHAR(64);

ALTER TABLE agent.interactions ADD COLUMN IF NOT EXISTS open_url TEXT;

ALTER TABLE agent.interactions ADD COLUMN IF NOT EXISTS request_summary JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX IF NOT EXISTS idx_agent_interactions_owner_interaction_id ON agent.interactions (owner_interaction_id);
