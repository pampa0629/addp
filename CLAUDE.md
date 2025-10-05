# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Structure

**ADDP (All Domain Data Platform / 全域数据平台)** is an enterprise data platform structured as microservices. Each service has its own directory:

- **common/** - Shared library module: common client code, models, and utilities used across all services
- **system/** - Core system module: user authentication, logging, resource management - **IMPLEMENTED** (PostgreSQL backend)
- **gateway/** - API gateway: handles external requests and routes to internal services - **IMPLEMENTED** (reverse proxy)
- **manager/** - Data management: data source connections, upload directory organization, data preview - **PARTIALLY IMPLEMENTED** (Go backend structure created)
- **meta/** - Metadata service: data metadata parsing/storage/querying, lineage tracking, extensible data type support, metadata-based search - *Planned*
- **transfer/** - Data transfer: data import/export/synchronization - *Planned*

All services follow the same architectural pattern and use shared infrastructure (PostgreSQL, Redis, MinIO). Common code is shared via the `common` module to avoid duplication.

## Quick Start

### Development (System Module Only)

```bash
# From system/ directory
cd system

# Backend development (需要 PostgreSQL)
cd backend && go run cmd/server/main.go

# Frontend development
cd frontend && npm install && npm run dev

# Docker deployment (System only)
make docker-up
```

### Full Platform Deployment

```bash
# From project root
make init           # Initialize config files
make up             # Start System module only
make up-full        # Start all services (Gateway + all modules + infrastructure)
make status         # Check service status
make logs           # View all logs
```

**For detailed System module documentation, see `system/CLAUDE.md`.**

## Technology Stack

### Backend
- **Language**: Go 1.21+
- **HTTP Framework**: Gin
- **ORM**: GORM
- **Databases**: SQLite (System module), PostgreSQL 15 (Manager/Meta/Transfer modules)
- **Cache/Queue**: Redis 7
- **Object Storage**: MinIO (S3-compatible)
- **Task Queue**: Asynq (Redis-based, for Transfer module)

### Frontend
- **Framework**: Vue 3 + Composition API
- **Build Tool**: Vite
- **UI Library**: Element Plus
- **State Management**: Pinia
- **Router**: Vue Router
- **HTTP Client**: Axios (with interceptors for auth)

### Infrastructure
- **Containerization**: Docker + Docker Compose
- **Reverse Proxy**: Nginx (production), Gateway service (API routing)
- **Database Schema Isolation**: PostgreSQL schemas (manager, metadata, transfer)

## Key Architectural Patterns

### Layered Backend Architecture (in system/backend/)

The Go backend follows a clean layered approach:

```
cmd/server/main.go          → Application entry point
internal/api/               → HTTP handlers + routing
internal/service/           → Business logic layer
internal/repository/        → Data access layer (GORM)
internal/models/            → Database models + DTOs
internal/middleware/        → Auth, logging middleware
pkg/utils/                  → Shared utilities (JWT, crypto)
```

**Data flow**: API Handler → Service → Repository → Database

### Frontend Architecture (Portal + Microservice Pattern)

**Unified Portal + Independent Module Frontends**:

The platform uses a **portal-based architecture** with a unified entry point:

```
portal/frontend/           → Unified Portal Entry (port 5170 dev / 8000 prod)
├── src/
│   ├── views/
│   │   ├── Portal.vue    → Main portal page with module cards
│   │   └── Login.vue     → Centralized login
│   ├── api/auth.js       → Authentication via System backend
│   └── router/           → Portal routes
│
│   Portal embeds module frontends via iframe:
│   - Left sidebar: Unified navigation for all modules
│   - Main area: iframe loading module frontends dynamically

system/frontend/           → System module (port 5173 dev / 8090 prod)
├── Standalone or embedded in portal
├── Features: Users, Logs, Resources

manager/frontend/          → Manager module (port 5174 dev / 8091 prod)
├── Standalone or embedded in portal
├── Features: DataSources, Directories, Preview

meta/frontend/             → Meta module (port 5175 dev / 8092 prod) - Planned
transfer/frontend/         → Transfer module (port 5176 dev / 8093 prod) - Planned
```

**Two Access Modes**:

1. **Unified Portal Mode** (Recommended for users):
   - Single entry: http://localhost:5170 (dev) or http://localhost:8000 (prod)
   - Integrated navigation with all modules
   - Module frontends load in iframe within portal
   - One login for all modules

2. **Standalone Module Mode** (For independent deployment):
   - Direct access to each module frontend
   - System: http://localhost:5173, Manager: http://localhost:5174
   - Each module has its own login
   - Suitable for deploying single module independently

**Key Frontend Principles**:
- Portal provides unified UX with consistent navigation
- Module frontends remain independent and can be deployed standalone
- All frontends share JWT auth pattern (token in localStorage)
- Portal and modules can authenticate independently
- In production, all requests route through Gateway (8000)

### Authentication Flow

1. User submits credentials to `POST /api/auth/login`
2. Backend validates with bcrypt, returns JWT (HS256, signed with `JWT_SECRET`)
3. Frontend stores token in localStorage (`auth.js` Pinia store)
4. Axios interceptor (`api/client.js`) adds `Authorization: Bearer <token>` to all requests
5. Backend `AuthMiddleware` validates JWT and injects user context into Gin context
6. Protected routes access user via `c.Get("user_id")`

### Database Architecture

**System Module (PostgreSQL - system schema)**:
- **users** - User accounts with bcrypt hashed passwords
- **tenants** - Tenant information for multi-tenancy
- **audit_logs** - Automatic logging of all non-GET operations (via `LoggerMiddleware`)
- **resources** - Flexible resource configurations with JSON connection_info field (encrypted)

**Shared Modules (PostgreSQL with schema isolation)**:
- **manager schema** - data_sources, directories, permissions tables
- **metadata schema** - datasets, fields, lineage tables
- **transfer schema** - tasks, task_executions, data_mappings tables

GORM AutoMigrate handles schema updates automatically on startup. PostgreSQL schemas initialized via `scripts/init-db.sql`.

### Configuration Center Pattern

**System as the Single Source of Truth**:

The platform implements a centralized configuration management pattern where **System module acts as the configuration center** for all other modules.

**Architecture**:
```
┌────────────────────────────────────────────────────┐
│   System Module (Configuration Center)            │
│                                                    │
│   ┌─────────────────────────────────────────┐    │
│   │  /internal/config API                   │    │
│   │  Returns: JWT_SECRET, DB connection,    │    │
│   │          ENCRYPTION_KEY                 │    │
│   └─────────────────────────────────────────┘    │
│                                                    │
│   ┌─────────────────────────────────────────┐    │
│   │  /api/resources (Business DB Config)    │    │
│   │  Manages all data source configs         │    │
│   │  (encrypted storage)                     │    │
│   └─────────────────────────────────────────┘    │
└────────────────┬───────────────────────────────────┘
                 │
      ┌──────────┼──────────┐
      ▼          ▼          ▼
  ┌────────┐ ┌────────┐ ┌─────────┐
  │ Manager│ │  Meta  │ │Transfer │
  │        │ │        │ │         │
  │ At     │ │ At     │ │ At      │
  │ Startup│ │ Startup│ │ Startup │
  │ ↓      │ │ ↓      │ │ ↓       │
  │ Get    │ │ Get    │ │ Get     │
  │ Config │ │ Config │ │ Config  │
  └────────┘ └────────┘ └─────────┘
```

**What is Centralized**:
1. **Authentication**: `JWT_SECRET` - ensures all services use the same JWT signing key
2. **System Database**: PostgreSQL connection info - single source for system data
3. **Business Databases**: Resources managed in System's `resources` table - all data source configs
4. **Encryption Key**: `ENCRYPTION_KEY` - consistent encryption across services

**Configuration Loading Flow**:
```
Module Startup
   ↓
Try to fetch config from System (/internal/config)
   ↓
   ├─ Success ✅
   │  └─ Use System config (JWT_SECRET, DB connection)
   │
   └─ Failure ⚠️
      └─ Fallback to local .env config
```

**Benefits**:
- ✅ **Single Source of Truth**: Change database password once, restart services to apply
- ✅ **Security**: Sensitive configs centrally managed and encrypted
- ✅ **Flexibility**: Supports both integrated and standalone deployment modes
- ✅ **Maintainability**: Reduced config duplication, easier to audit

**SystemClient Usage**:

All modules use `SystemClient` to fetch business database configurations from System:

```go
// Create client with JWT token
client := utils.NewSystemClient(systemURL, jwtToken)

// List all data sources
resources, err := client.ListResources("postgresql")

// Get specific data source
resource, err := client.GetResource(resourceID)

// Build connection string (password auto-decrypted)
connStr, err := utils.BuildConnectionString(resource)
```

**Module .env Files**:

Each module only needs to configure module-specific settings:

```bash
# Manager/Meta/Transfer .env
PORT=8081                          # Module-specific port
DB_SCHEMA=manager                  # Module-specific schema
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true    # Enable config center

# Shared configs (JWT_SECRET, DB connection) fetched from System
# Fallback configs commented out (used only when integration disabled)
```

**See Also**: `docs/CONFIG_CENTER.md` for detailed configuration center usage guide.

## Common Module

The `common` module provides shared code used across Manager, Meta, and Transfer modules to avoid code duplication.

**Contents**:
- `client/system.go` - SystemClient for communicating with System module
- `models/resource.go` - Shared Resource model and BuildConnectionString utility

**Usage in modules**:
```go
// In go.mod
require (
    github.com/addp/common v0.0.0
)
replace github.com/addp/common => ../../common

// In code
import (
    "github.com/addp/common/client"
    commonModels "github.com/addp/common/models"  // Use alias if needed
)

sysClient := client.NewSystemClient(systemURL, token)
resource, err := sysClient.GetResource(resourceID)
connStr, err := commonModels.BuildConnectionString(resource)
```

**See Also**: `docs/COMMON_MODULE.md` for detailed common module documentation.

## Development Workflows

### Adding New API Endpoints

1. Define request/response structs in `internal/models/`
2. Add repository methods in `internal/repository/`
3. Implement business logic in `internal/service/`
4. Create HTTP handler in `internal/api/`
5. Register route in `internal/api/router.go`

### Database Migrations

1. Modify model struct in `internal/models/`
2. Add model to AutoMigrate list in `repository/database.go`
3. Restart application (migration runs automatically)

### Adding Frontend Pages

**Important**: Add pages to the correct frontend based on functionality:
- System features (users, logs, resources) → `system/frontend/`
- Manager features (data sources, directories) → `manager/frontend/`
- Meta features (metadata, lineage) → `meta/frontend/`
- Transfer features (tasks, executions) → `transfer/frontend/`

Steps for each frontend:
1. Create Vue component in `<module>/frontend/src/views/`
2. Add API functions in `<module>/frontend/src/api/`
3. Register route in `<module>/frontend/src/router/index.js`
4. Add navigation link in `<module>/frontend/src/components/Layout.vue`

## Configuration

### Environment Variables

Root `.env` file (copy from `.env.example`):
```bash
# Security (MUST change for production)
JWT_SECRET=your-super-secret-jwt-key-change-this-in-production

# PostgreSQL (for Manager, Meta, Transfer)
POSTGRES_PASSWORD=addp_password
POSTGRES_USER=addp
POSTGRES_DB=addp

# Redis
REDIS_PASSWORD=addp_redis

# MinIO
MINIO_ROOT_PASSWORD=minioadmin

# Service Integration
ENABLE_SERVICE_INTEGRATION=true  # Enable cross-service calls
```

### Port Assignments

| Service | Dev Port | Docker Port | Description |
|---------|----------|-------------|-------------|
| **Portal Frontend** | **5170** | **8000** | **Unified entry point** |
| Gateway | 8000 | 8000 | API Gateway (backend routing) |
| System Backend | 8080 | 8080 | Auth, users, logs |
| System Frontend | 5173 | 8090 | Standalone access |
| Manager Backend | 8081 | 8081 | Data sources, files |
| Manager Frontend | 5174 | 8091 | Standalone access |
| Meta Backend | 8082 | 8082 | Metadata, lineage |
| Meta Frontend | 5175 | 8092 | Standalone access |
| Transfer Backend | 8083 | 8083 | Import/export tasks |
| Transfer Frontend | 5176 | 8093 | Standalone access |
| PostgreSQL | 5432 | 5432 | Shared database |
| Redis | 6379 | 6379 | Cache & queue |
| MinIO API | 9000 | 9000 | Object storage |
| MinIO Console | 9001 | 9001 | MinIO web UI |

**Recommended Access**: Use Portal at **http://localhost:5170** for unified experience

## Testing

```bash
# Test all modules (from project root)
make test

# Test specific module
cd system/backend && go test ./...
cd manager/backend && go test ./...

# Test with coverage
go test -cover ./...

# Test specific package
go test ./internal/service/...
```

## Docker Deployment

### System Module Only (Default)

```bash
# From project root
make up              # Start System backend + frontend
make logs-system     # View logs
make down           # Stop services
```

### Full Platform

```bash
# From project root
make up-full        # Start all services with --profile full
make status         # Check all service status
make logs           # View all logs
make down           # Stop all services

# Individual service logs
make logs-system
make logs-manager
make logs-gateway
```

### Rebuild After Changes

```bash
make docker-build       # Rebuild System only
make docker-build-all   # Rebuild all services
docker-compose up -d    # Restart
```

**Data Persistence**:
- PostgreSQL: `postgres_data` volume (includes system schema for System module)
- Redis: `redis_data` volume
- MinIO: `minio_data` volume

## API Endpoints Summary

**Public**:
- `POST /api/auth/login` - Login
- `POST /api/auth/register` - Register

**Protected** (require JWT):
- `GET /api/users/me` - Current user
- `GET /api/users` - List users
- `GET/PUT/DELETE /api/users/:id` - User CRUD
- `GET /api/logs` - Audit logs (supports `?user_id=X` filter)
- `POST/GET/PUT/DELETE /api/resources` - Resource CRUD (supports `?resource_type=X` filter)

## Service Architecture Details

### Gateway Service (IMPLEMENTED)
**Purpose**: Unified API entry point for all microservices

**Key Features**:
- HTTP reverse proxy using Gin
- Route matching by URL prefix (`/api/auth/*` → System, `/api/datasources/*` → Manager, etc.)
- CORS middleware for cross-origin requests
- Transparent request/response forwarding (headers, body, query params preserved)
- Health check endpoint at `/health`

**Configuration**: Service URLs configured via environment variables (`SYSTEM_SERVICE_URL`, `MANAGER_SERVICE_URL`, etc.)

**Architecture Files**: See `gateway/ARCHITECTURE.md` for detailed request flow and routing rules

### Manager Service (PARTIAL)
**Purpose**: Data source management and file organization

**Planned Features**:
- Connect to various data sources (MySQL, PostgreSQL, S3, HDFS)
- Hierarchical directory structure for file organization
- Multi-format data preview (CSV, JSON, Parquet, Excel)
- Permission-based access control (user/group level)
- Integration with System module for authentication
- Integration with Meta module for metadata extraction

**Database**: PostgreSQL `manager` schema (tables: data_sources, directories, permissions)

### Meta Service (PLANNED)
**Purpose**: Metadata management and data lineage

**Planned Features**:
- Automatic metadata extraction from various data formats
- Schema registry and data catalog
- Field-level metadata and statistics
- Data lineage tracking (source → transformation → target)
- Tag-based search and discovery
- Extensible parser plugins for new data types

**Database**: PostgreSQL `metadata` schema (tables: datasets, fields, lineage)

### Transfer Service (PLANNED)
**Purpose**: Data import/export and synchronization

**Planned Features**:
- Import from external sources (databases, APIs, files)
- Export to various targets
- Scheduled tasks with Cron expressions
- Field mapping and transformations
- Batch processing with progress tracking
- Asynq-based task queue for async execution
- Retry mechanism for failed transfers

**Database**: PostgreSQL `transfer` schema (tables: tasks, task_executions, data_mappings)

## Inter-Service Communication

**Current Pattern**: HTTP REST calls between services
- Services discover each other via environment variables (e.g., `SYSTEM_SERVICE_URL`)
- Manager/Meta/Transfer can call System APIs for user validation
- Manager notifies Meta when new data sources are added
- Transfer queries Manager for data source connection info

**Auth Propagation**: JWT tokens passed through in `Authorization` headers

**Error Handling**: Services return standard HTTP status codes; calling services handle retries

## Development Guidelines for New Services

When implementing or extending services:

1. **Follow System module pattern**:
   ```
   service/backend/
   ├── cmd/server/main.go       # Entry point
   ├── internal/
   │   ├── api/                 # HTTP handlers
   │   ├── service/             # Business logic
   │   ├── repository/          # Data access
   │   ├── models/              # Data structures
   │   ├── middleware/          # Auth, logging
   │   └── config/              # Configuration
   └── pkg/utils/               # Shared utilities
   ```

2. **Database conventions**:
   - Use PostgreSQL schema isolation (except System which uses SQLite)
   - GORM for ORM with AutoMigrate
   - Add schemas to `scripts/init-db.sql`
   - Use `updated_at` triggers for timestamp tracking

3. **Configuration**:
   - Read from environment variables via `internal/config/config.go`
   - Support both development and Docker deployment modes
   - Set defaults for missing env vars

4. **Authentication**:
   - Reuse System module's JWT validation logic
   - Import auth middleware from System or create identical one
   - Extract user_id from JWT claims and pass to service layer

5. **Docker integration**:
   - Create Dockerfile in service root
   - Add service to `docker-compose.yml` with `profile: full`
   - Use health checks for dependency management
   - Connect to `addp-network` for inter-service communication

6. **Frontend integration**:
   - Create independent `<module>/frontend/` directory
   - Copy structure from `system/frontend/` (Vue 3 + Pinia + Element Plus)
   - Create `api/client.js` pointing to module's backend
   - Create `api/auth.js` pointing to System backend (8080) for authentication
   - Copy auth store pattern from System module (independent copy, not shared)
   - Set unique dev port in `vite.config.js` (System: 5173, Manager: 5174, etc.)
   - Configure router base path (e.g., `/manager/` for Manager module)
   - Create Dockerfile and nginx.conf for production deployment
   - Add to docker-compose.yml with unique port and `profile: full`

## Frontend Development Workflow

### Quick Start: Portal + All Modules

```bash
# Terminal 1: Start Portal (unified entry)
cd portal/frontend
npm install
npm run dev
# Access: http://localhost:5170

# Terminal 2: Start System frontend
cd system/frontend
npm install
npm run dev

# Terminal 3: Start Manager frontend
cd manager/frontend
npm install
npm run dev

# Now visit http://localhost:5170 for unified experience
# All modules accessible through single portal interface
```

### Running Individual Frontends (Standalone Mode)

```bash
# System frontend (port 5173)
cd system/frontend
npm run dev
# Access: http://localhost:5173

# Manager frontend (port 5174)
cd manager/frontend
npm run dev
# Access: http://localhost:5174

# Portal (port 5170)
cd portal/frontend
npm run dev
# Access: http://localhost:5170
```

### Frontend-Backend Connection in Development

**Development mode** (direct backend connection):
- System frontend → System backend (localhost:8080)
- Manager frontend → Manager backend (localhost:8081)
- Auth requests → System backend (localhost:8080)

**Production mode** (via Gateway):
- All frontend requests → Gateway (localhost:8000)
- Gateway routes to appropriate backend

### Creating New Module Frontend

When implementing a new module (e.g., Meta), follow these steps:

1. **Copy frontend structure**:
   ```bash
   cp -r system/frontend meta/frontend
   cd meta/frontend
   ```

2. **Update configuration**:
   - `package.json`: Change name to `meta-frontend`
   - `vite.config.js`: Change port to unique number (e.g., 5175)
   - `index.html`: Update title
   - `src/router/index.js`: Set base path to `/meta/`
   - `src/api/client.js`: Point baseURL to meta backend (8082)
   - Keep `src/api/auth.js` pointing to System backend (8080)

3. **Update views and components** to match module's functionality

4. **Add Dockerfile and nginx.conf** (copy from manager/frontend as template)

5. **Add to docker-compose.yml**:
   ```yaml
   meta-frontend:
     build:
       context: ./meta/frontend
     ports:
       - "8092:80"
     profiles:
       - full
   ```

## Common Make Commands (Project Root)

```bash
# Initialization
make init                # Create config files and directories
make install-deps        # Install Go and npm dependencies

# Development
make dev-system          # Run System in development mode
make dev-manager         # Run Manager backend
make dev-gateway         # Run Gateway service

# Docker Operations
make up                  # Start System module only
make up-full             # Start all services (full platform)
make up-infra            # Start only PostgreSQL, Redis, MinIO
make down                # Stop all services
make restart             # Restart System module
make restart-full        # Restart all services

# Building
make build               # Build all Go binaries to bin/
make docker-build        # Build System Docker images
make docker-build-all    # Build all service Docker images

# Monitoring
make status              # Show all service status and URLs
make logs                # View all service logs
make logs-system         # View System logs only
make logs-manager        # View Manager logs
make health              # Check health of all services

# Database
make db-shell            # Connect to PostgreSQL
make db-migrate          # Run database migrations (init-db.sql)
make redis-cli           # Connect to Redis
make minio-setup         # Initialize MinIO buckets
make backup              # Backup PostgreSQL database
make restore FILE=...    # Restore database from backup

# Testing & Quality
make test                # Run all tests
make test-system         # Run System module tests
make lint                # Run code linters
make fmt                 # Format Go code

# Cleanup
make clean               # Remove build artifacts
make clean-all           # Remove all data and volumes (DESTRUCTIVE)
```

## Important File Locations

- **Main config**: `.env` (root) - shared environment variables
- **Database init**: `scripts/init-db.sql` - PostgreSQL schema setup
- **Docker compose**: `docker-compose.yml` (root) - all service definitions
- **Root Makefile**: `Makefile` (root) - orchestration commands
- **System docs**: `system/CLAUDE.md` - detailed System module documentation
- **Gateway docs**: `gateway/ARCHITECTURE.md` - gateway implementation details

## Troubleshooting

**Services won't start**:
```bash
make status              # Check what's running
docker-compose ps        # Check container status
make logs                # Check for errors
```

**Port conflicts**:
```bash
lsof -i :8080            # Check what's using port 8080
# Kill process or change port in docker-compose.yml
```

**Database connection issues**:
```bash
docker-compose ps postgres    # Ensure PostgreSQL is running
make db-shell                 # Try connecting manually
docker-compose restart postgres
```

**Cannot access MinIO**:
```bash
make minio-setup         # Initialize MinIO buckets
curl http://localhost:9001   # Check MinIO console
```

**JWT token issues**: Ensure `JWT_SECRET` in `.env` matches between services (System and Gateway need same secret)

**Cross-service calls failing**: Verify `ENABLE_SERVICE_INTEGRATION=true` and service URLs are correct in docker-compose.yml