-- Business Database Initialization Script
-- 业务数据库初始化脚本
--
-- 此脚本在 PostgreSQL 容器首次启动时自动执行
-- 用于初始化业务数据库的基础结构

-- Enable PostGIS extension for spatial data support
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS postgis_topology;

-- Optional: Enable other useful extensions
-- CREATE EXTENSION IF NOT EXISTS "uuid-ossp";  -- UUID generation
-- CREATE EXTENSION IF NOT EXISTS hstore;        -- Key-value store
-- CREATE EXTENSION IF NOT EXISTS pg_trgm;       -- Trigram matching for fuzzy search

-- Grant permissions
GRANT ALL PRIVILEGES ON DATABASE business TO business;

-- Display installed extensions
\dx

-- Display PostGIS version
SELECT PostGIS_Version();

-- Success message
\echo 'Business database initialization complete!'
\echo 'PostGIS extension is ready for spatial data.'
