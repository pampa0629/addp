-- Business Database Initialization Script
-- 业务数据库初始化脚本
--
-- 业务数据库用于存储用户通过 ADDP 管理的实际业务数据
-- 保留默认的 public schema，不创建额外的 schema 或表
-- 实际的业务表由用户上传数据时动态创建

-- Enable PostGIS extension (空间数据支持)
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS postgis_topology;

-- 提示信息
SELECT 'Business database initialized with PostGIS support. Using default public schema.' AS info;
