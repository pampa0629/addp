#!/bin/bash
# Business Database Initialization Script
# 业务数据库初始化脚本

set -e

# 运行初始化 SQL
psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" <<-EOSQL
    -- Enable PostGIS extension for spatial data support
    CREATE EXTENSION IF NOT EXISTS postgis;
    CREATE EXTENSION IF NOT EXISTS postgis_topology;

    -- Grant permissions
    GRANT ALL PRIVILEGES ON DATABASE business TO business;

    -- Display installed extensions
    SELECT * FROM pg_extension;

    -- Display PostGIS version
    SELECT PostGIS_Version();

    -- Success message
    \echo 'Business database initialization complete!'
    \echo 'PostGIS extension is ready for spatial data.'
EOSQL

echo "✓ Database initialization completed"
