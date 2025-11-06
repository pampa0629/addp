-- Business Database Initialization Script
-- 业务数据库初始化脚本
--
-- 用于初始化业务数据库的基础 schema 和权限设置
-- 实际的业务表会由各个数据源动态创建

-- Install PostGIS extension for spatial data support
-- 安装 PostGIS 扩展以支持空间数据
-- 注意: postgres:15-alpine 镜像不包含 PostGIS
-- 如需空间数据支持,请使用 postgis/postgis 镜像或手动安装 PostGIS
-- CREATE EXTENSION IF NOT EXISTS postgis;
-- CREATE EXTENSION IF NOT EXISTS postgis_topology;

-- Create default schemas for different types of business data
-- 为不同类型的业务数据创建默认 schema

-- Schema for uploaded PostgreSQL data sources
CREATE SCHEMA IF NOT EXISTS business_data;

-- Schema for temporary staging data
CREATE SCHEMA IF NOT EXISTS staging;

-- Schema for archived data
CREATE SCHEMA IF NOT EXISTS archive;

-- Set default privileges
-- Note: CURRENT_USER is set by POSTGRES_USER environment variable
GRANT ALL PRIVILEGES ON SCHEMA business_data TO CURRENT_USER;
GRANT ALL PRIVILEGES ON SCHEMA staging TO CURRENT_USER;
GRANT ALL PRIVILEGES ON SCHEMA archive TO CURRENT_USER;

-- Create a metadata table to track business data sources
CREATE TABLE IF NOT EXISTS business_data.data_source_registry (
    id SERIAL PRIMARY KEY,
    source_name VARCHAR(255) NOT NULL UNIQUE,
    source_type VARCHAR(50) NOT NULL, -- 'postgresql', 'mysql', 'shapefile', etc.
    schema_name VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    metadata JSONB DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_data_source_type ON business_data.data_source_registry(source_type);
CREATE INDEX IF NOT EXISTS idx_data_source_schema ON business_data.data_source_registry(schema_name);

COMMENT ON TABLE business_data.data_source_registry IS '业务数据源注册表 - 记录所有通过ADDP管理的业务数据源';
COMMENT ON COLUMN business_data.data_source_registry.source_name IS '数据源名称（唯一标识）';
COMMENT ON COLUMN business_data.data_source_registry.source_type IS '数据源类型';
COMMENT ON COLUMN business_data.data_source_registry.schema_name IS '数据存储的schema名称';
COMMENT ON COLUMN business_data.data_source_registry.metadata IS '数据源元数据（JSON格式）';
