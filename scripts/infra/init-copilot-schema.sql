-- ========================================
-- Copilot Schema 初始化脚本
-- ========================================

-- 创建 copilot schema
CREATE SCHEMA IF NOT EXISTS copilot;

-- ========================================
-- 对话会话表
-- ========================================
CREATE TABLE IF NOT EXISTS copilot.conversations (
    id SERIAL PRIMARY KEY,
    tenant_id INT NOT NULL,
    user_id INT NOT NULL,
    context_type VARCHAR(32) NOT NULL,  -- 'sql' 或 'workflow'
    status VARCHAR(32) DEFAULT 'active',  -- 'active', 'archived'
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_conversations_tenant_user ON copilot.conversations(tenant_id, user_id);
CREATE INDEX IF NOT EXISTS idx_conversations_context_type ON copilot.conversations(context_type);

COMMENT ON TABLE copilot.conversations IS 'Copilot 对话会话表';
COMMENT ON COLUMN copilot.conversations.context_type IS '对话上下文类型: sql 或 workflow';

-- ========================================
-- 对话消息表
-- ========================================
CREATE TABLE IF NOT EXISTS copilot.messages (
    id SERIAL PRIMARY KEY,
    conversation_id INT NOT NULL REFERENCES copilot.conversations(id) ON DELETE CASCADE,
    role VARCHAR(20) NOT NULL,  -- 'user', 'assistant', 'system'
    content TEXT NOT NULL,
    extra_data JSONB,  -- 附加信息（生成的 SQL、选中的数据源等）
    token_count INT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_messages_conversation ON copilot.messages(conversation_id);
CREATE INDEX IF NOT EXISTS idx_messages_role ON copilot.messages(role);

COMMENT ON TABLE copilot.messages IS 'Copilot 对话消息表';
COMMENT ON COLUMN copilot.messages.extra_data IS '附加信息: 生成的 SQL、选中的数据源、工作流 DAG 等';

-- ========================================
-- LLM 配置表
-- ========================================
CREATE TABLE IF NOT EXISTS copilot.llm_configs (
    id SERIAL PRIMARY KEY,
    tenant_id INT,  -- NULL 表示全局配置
    provider VARCHAR(50) NOT NULL,  -- 'openai', 'claude', 'ollama', 'dashscope'
    model VARCHAR(100) NOT NULL,
    api_key TEXT,  -- AES 加密存储
    base_url VARCHAR(512),
    config JSONB,  -- 额外配置（temperature, max_tokens 等）
    is_default BOOLEAN DEFAULT false,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, provider, model)
);

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_llm_configs_tenant ON copilot.llm_configs(tenant_id);
CREATE INDEX IF NOT EXISTS idx_llm_configs_provider ON copilot.llm_configs(provider);

COMMENT ON TABLE copilot.llm_configs IS 'LLM 配置表（支持租户级配置）';
COMMENT ON COLUMN copilot.llm_configs.tenant_id IS '租户 ID，NULL 表示全局配置';
COMMENT ON COLUMN copilot.llm_configs.provider IS 'LLM 提供商: openai, claude, ollama, dashscope';

-- ========================================
-- 插入默认配置（通义千问）
-- ========================================
INSERT INTO copilot.llm_configs (tenant_id, provider, model, is_default, is_active, config)
VALUES (
    NULL,  -- 全局配置
    'dashscope',
    'qwen-max',
    true,
    true,
    '{"temperature": 0.7, "max_tokens": 2000}'::jsonb
)
ON CONFLICT (tenant_id, provider, model) DO NOTHING;

COMMENT ON COLUMN copilot.llm_configs.config IS '额外配置 JSON: temperature, max_tokens, top_p 等';

-- ========================================
-- 完成
-- ========================================
GRANT USAGE ON SCHEMA copilot TO addp_user;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA copilot TO addp_user;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA copilot TO addp_user;
