# =============================================================================
# ADDP Custom PostgreSQL Image
# =============================================================================
# Description: PostgreSQL 15 with built-in ADDP database initialization script
# Base Image: postgres:15-alpine
# =============================================================================

FROM postgres:15-alpine

# Metadata
LABEL maintainer="ADDP Team"
LABEL description="PostgreSQL 15 with ADDP schema initialization"
LABEL version="0.0.6"

# Copy initialization script
# Scripts in /docker-entrypoint-initdb.d/ are executed on first container start
COPY init-db.sql /docker-entrypoint-initdb.d/

# Set correct permissions
RUN chmod 644 /docker-entrypoint-initdb.d/init-db.sql

# Health check
HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=3 \
    CMD pg_isready -U ${POSTGRES_USER:-postgres} || exit 1

# Expose PostgreSQL port
EXPOSE 5432

# Use default postgres entrypoint
# This will run init-db.sql on first startup automatically
