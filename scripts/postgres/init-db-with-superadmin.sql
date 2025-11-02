-- ============================================================================= -- ADDP Database Initialization Script
-- Description: Initialize database schemas and tables for all ADDP modules
-- Modules: System, Manager, Meta, Transfer
-- Generated: 2025-10-31
-- =============================================================================

-- ==================== System 模块 ====================
CREATE SCHEMA IF NOT EXISTS system;

-- 用户表
CREATE TABLE IF NOT EXISTS system.users (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    password VARCHAR(255) NOT NULL,
    email VARCHAR(100) UNIQUE,
    full_name VARCHAR(100),
    role VARCHAR(20) DEFAULT 'user',
    status VARCHAR(20) DEFAULT 'active',
    avatar_url TEXT,
    last_login_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_users_username ON system.users(username);
CREATE INDEX IF NOT EXISTS idx_users_email ON system.users(email);
CREATE INDEX IF NOT EXISTS idx_users_role ON system.users(role);

-- 租户表
CREATE TABLE IF NOT EXISTS system.tenants (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    code VARCHAR(50) UNIQUE NOT NULL,
    description TEXT,
    status VARCHAR(20) DEFAULT 'active',
    admin_user_id BIGINT REFERENCES system.users(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 资源表（数据源配置）
CREATE TABLE IF NOT EXISTS system.resources (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT REFERENCES system.tenants(id),
    name VARCHAR(255) NOT NULL,
    resource_type VARCHAR(50) NOT NULL,
    connection_info JSONB NOT NULL,
    description TEXT,
    status VARCHAR(20) DEFAULT 'active',
    tags TEXT[],
    created_by BIGINT REFERENCES system.users(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_resources_type ON system.resources(resource_type);
CREATE INDEX IF NOT EXISTS idx_resources_tenant ON system.resources(tenant_id);
CREATE INDEX IF NOT EXISTS idx_resources_created_by ON system.resources(created_by);

-- 审计日志表
CREATE TABLE IF NOT EXISTS system.audit_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT REFERENCES system.users(id),
    tenant_id BIGINT,
    action VARCHAR(50) NOT NULL,
    resource_type VARCHAR(50),
    resource_id BIGINT,
    method VARCHAR(10),
    path TEXT,
    ip_address INET,
    user_agent TEXT,
    request_body TEXT,
    response_status INTEGER,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_user ON system.audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON system.audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON system.audit_logs(created_at);

-- 配置表
CREATE TABLE IF NOT EXISTS system.configs (
    id BIGSERIAL PRIMARY KEY,
    key VARCHAR(100) UNIQUE NOT NULL,
    value TEXT NOT NULL,
    description TEXT,
    is_encrypted BOOLEAN DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
