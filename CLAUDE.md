# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Communication Language / 交流语言

**Important**: In this project, **please communicate with users in Chinese as much as possible**. Use English only when the user explicitly asks questions in English.

**重要**: 在此项目中,**请尽可能使用中文与用户交流**。除非用户明确使用英文提问,否则默认使用中文回复。

## 📚 Documentation Navigation Map (For Claude Code)

**When encountering the following scenarios, proactively read the corresponding documentation**:


| Scenario                        | Required Documentation         | Trigger Keywords                           |
| ------------------------------- | ------------------------------ | ------------------------------------------ |
| Development principles & coding standards | docs/addp开发原则.md           | principles, standards, DRY, backward compatibility |
| Environment config, ports, keys | docs/addp配置介绍.md           | config, ports, environment variables, .env |
| Go/Frontend dependency versions | docs/技术栈规约.md             | dependencies, versions, upgrades, libraries |
| Common module usage             | docs/共享模块介绍.md           | common, shared code, reuse                 |
| Creating new modules            | docs/新模块开发指南.md         | new module, scaffolding, templates         |
| Deployment and startup          | docs/addp部署和开发步骤.md     | deployment, startup, docker, scripts       |
| System module details           | system/CLAUDE.md               | authentication, users, tenants, logs       |
| Gateway routing                 | gateway/ARCHITECTURE.md        | routing, forwarding, API gateway           |

**Important**:

- When encountering related issues, read the corresponding documentation first
- Multiple scenarios may require reading multiple documents
- External link documents contain detailed information, main document provides overview only

## Development Principles

**Important**: ADDP is currently under active development. The following principles guide all development work:

### 1. No Backward Compatibility Concerns

During development phase, prioritize best architecture. Freely modify database schemas and API structures.

### 2. Keep It Clean

Save temporary scripts and documents to /tmp/, keep project tree clean.

### 3. No Need to Ask Permission

In auto-edit mode, freely execute scripts without asking user each time.

### 4. Think Holistically

Any modification must consider global impact, synchronously update related modules.

### 5. Fix Root Causes

Deeply analyze root causes, solve completely rather than apply temporary patches.

### 6. Be Bold to Delete

Delete obsolete code and files, don't keep "just in case" content.

### 7. Challenge Unreasonable Requests

Question unreasonable requirements, discuss equally to reach consensus.

### 8. DRY (Don't Repeat Yourself / 不要重复自己)

Extract repeated code to common/ or common-frontend/ modules.

Detailed explanation: docs/addp开发原则.md

## Repository Structure

**ADDP (All Domain Data Platform / 全域数据平台)** is an enterprise data platform with microservices architecture. Each service has its own directory:

- **common/** - Backend shared library: common client code, models, config loader, and utilities used by all backend services
- **common-frontend/** - Frontend shared library: Vue 3 components, utilities, and type definitions for frontend reuse
  - **basic/** - Basic UI components without map dependencies (StorageEngineForm, ImagePreview, formatters)
  - **map/** - Map-related components requiring OpenLayers and Gaode Map (GeoJsonPreview, ShapefilePreview, TablePreview)
- **portal/** - Unified portal entry point with iframe-based module integration - **IMPLEMENTED**
- **system/** - Core system module: user authentication, logging, resource management - **IMPLEMENTED** (PostgreSQL system schema)
- **gateway/** - API gateway: handles external requests and routes to internal services - **IMPLEMENTED** (reverse proxy)
- **manager/** - Data management: data source connections, upload directory organization, data preview - **IMPLEMENTED**
- **meta/** - Metadata service: data metadata parsing/storage/querying, cron-scheduled scanning - **IMPLEMENTED** (PostgreSQL metadata schema)
- **transfer/** - Data transfer: data import/export/synchronization - **IMPLEMENTED**
- **orchestrator/** - Workflow orchestration: task scheduling and execution - **IMPLEMENTED**
- **develop/** - Development workbench: SQL execution, GIS workflow management - **IMPLEMENTED**
- **geopandas-engine/** - Spatial computation engine: Python-based GIS workflow execution, providing 21 spatial operators - **IMPLEMENTED**

All services follow the same architectural pattern and use shared infrastructure (PostgreSQL, Redis, MinIO, Meilisearch). Common code is shared via the `common` module (backend) and `common-frontend` module (frontend) to avoid duplication.

## Quick Start

### Basic Startup (3 Steps)

1. **Start infrastructure**: `bash scripts/infra/up.sh`
2. **Start development environment**: `bash scripts/dev/start.sh`
3. **Access application**:
   - Portal unified entry: http://localhost:5170
   - API Gateway: http://localhost:8000
   - System Backend: http://localhost:8080

Detailed steps: docs/addp部署和开发步骤.md

## Module Ports

**Core Ports** (High-frequency usage):

- **Portal**: 5170 (dev) / 80 (prod via Nginx)
- **Gateway**: 8000
- **System Backend**: 8080
- **PostgreSQL**: 5432 (system)
- **Redis**: 6379
- **MinIO**: 9000-9001 (system) / 9002-9003 (business)

Complete port list: docs/addp配置介绍.md

## Technology Stack

### Backend

- **Language**: Go 1.23+
- **HTTP Framework**: Gin
- **ORM**: GORM
- **Database**: PostgreSQL 15 (all modules use schema isolation: system, manager, metadata, transfer, orchestrator, develop)
- **Cache/Queue**: Redis 7
- **Object Storage**: MinIO (S3-compatible)
- **Task Queue**: Asynq (Redis-based, for Transfer module), Cron (for Meta module scheduling)
- **Spatial Computation**: GeoPandas Engine (Python-based spatial workflow execution engine with in-memory GeoDataFrame processing)

### Go Dependency Version Standards

To ensure dependency version consistency across all modules, ADDP platform uses unified Go dependency versions (last updated: 2025-12-15).
When detailed technical stack information is needed, please refer to docs/技术栈规约.md document.

### Infrastructure

- **Containerization**: Docker + Docker Compose
- **Reverse Proxy**: Nginx (production), Gateway service (API routing)
- **Database Schema Isolation**: PostgreSQL schemas (manager, metadata, transfer)
- **Data Separation**: System infrastructure (ADDP metadata) + Business database (user data) independently deployed

### Infrastructure Architecture

ADDP adopts **system and business data separation** architecture design:

**System Infrastructure** (docker-compose.infra.yml):

- **Docker Compose Project Name**: `addp-infra`
- **Container Naming**: Simple names (postgres, redis, minio, meilisearch), managed by project name for isolation
- **postgres**: Stores ADDP system metadata (users, resource configs, metadata indexes, task definitions, etc.)
- **redis**: Cache and task queue (Asynq)
- **minio**: Stores system files (user avatars, system configs, modular buckets)
- **meilisearch**: Full-text search engine (metadata asset search, file indexing)

**Business Database** (business/docker-compose.yml, independently deployed):

- `business-postgres`: Stores actual business data managed by users through ADDP (user-uploaded PostgreSQL data, etc.)
- `business-minio`: Stores user-uploaded business files (Shapefile, GeoJSON, images, videos, etc.)

### Module-Based Resource Isolation

ADDP adopts **modular resource isolation** strategy to ensure independent module resource management:
**PostgreSQL Schema Isolation**: Isolated by module name
**MinIO Bucket Isolation**: Isolated by module name
**Redis Key Naming Convention**: {module}:{middleware}:{function}:{id}
**Asynq Queue Naming Convention**: {module}:{priority}
**Meilisearch Index Naming Convention**: {module}:{resource_type}

## Key Architectural Patterns

### Layered Backend Architecture (in system/backend/)

Go backend follows clean layered approach:

```
cmd/server/main.go          → Application entry point
internal/api/               → HTTP handlers + routing
internal/service/           → Business logic layer
internal/repository/        → Data access layer (GORM)
internal/models/            → Database models + DTOs
internal/middleware/        → Auth, logging middleware
pkg/utils/                  → Shared utilities (JWT, encryption)
```

**Data Flow**: API Handler → Service → Repository → Database

### Frontend Architecture (Portal + Microservice Pattern)

**Unified Portal + Independent Module Frontends**:

Platform uses **portal-based architecture** providing unified entry point:

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
│   - Main area: iframe dynamically loads module frontends

system/frontend/           → System module (port 5173 dev / 8090 prod)
├── Standalone or embedded in portal
├── Features: Users, Logs, Resources
Other modules similar to system.
```

**Two Access Modes**:

1. **Unified Portal Mode** (Recommended for users):

   - Single entry: http://localhost:5170 (dev) or http://localhost:8000 (prod)
   - Integrated navigation with all modules
   - Module frontends load in portal's iframe
   - One login for all modules
2. **Standalone Module Mode** (For independent deployment):

   - Direct access to each module frontend
   - System: http://localhost:5173, Manager: http://localhost:5174
   - Each module has its own login
   - Suitable for deploying single module independently

**Key Frontend Principles**:

- Portal provides unified user experience and consistent navigation
- Module frontends remain independent, can be deployed standalone
- All frontends share JWT auth pattern (token stored in localStorage)
- Portal and modules can authenticate independently
- In production, all requests route through Gateway (8000)

### Authentication Flow

JWT authentication pattern: User login → Backend validates → Returns JWT → Frontend stores token → Requests carry token → Backend validates token

### Test Accounts

**Tenant Admin**: `admin` / `123456` (Manages users, resources, data of default tenant)
**Super Admin**: `SuperAdmin` / `20251001#SuperAdmin` (System-level management, tenant management)

Detailed explanation and enabling method: system/CLAUDE.md

### Configuration Center Pattern

When detailed content is needed, please read docs/addp配置介绍.md

### Shared Modules

The `common` module provides shared code to avoid duplication across **all other backend modules** (Manager, Meta, Transfer, Orchestrator, Develop, and GeoPandas Engine integration).
The `common-frontend` module provides shared Vue 3 components, utilities, and type definitions for cross-module frontend reuse.
For detailed introduction of Common module and common-frontend module, please read when needed: docs/共享模块介绍.md

### New Module Development

When developing new modules, please read: docs/新模块开发指南.md

## Important File Locations

### Configuration

- [`.env`](.env) - Root environment variables (shared config)
- [`.env.example`](.env.example) - Template with all available options
- [`docker-compose.yml`](docker-compose.yml) - Service definitions and networking

### Documentation

- [`CLAUDE.md`](CLAUDE.md) - This file (platform-wide architecture)
- [`system/CLAUDE.md`](system/CLAUDE.md) - System module details
- [`gateway/ARCHITECTURE.md`](gateway/ARCHITECTURE.md) - Gateway routing logic
- [`docs/CONFIG_CENTER.md`](docs/CONFIG_CENTER.md) - Configuration center guide
- [`docs/COMMON_MODULE.md`](docs/COMMON_MODULE.md) - Common module usage
- [`common-frontend/README.md`](common-frontend/README.md) - Common frontend components guide
- [`common-frontend/ARCHITECTURE.md`](common-frontend/ARCHITECTURE.md) - Common frontend architecture
- DATA_STRUCTURES.md in each module directory

### Build and Deployment

When needed, please read docs/addp部署和开发步骤.md

### Key Source Files

- System authentication: [system/backend/internal/middleware/auth.go](system/backend/internal/middleware/auth.go)
- Manager preview: [manager/backend/internal/service/object_preview.go](manager/backend/internal/service/object_preview.go)
- Meta scanning: [meta/backend/internal/service/scan_service.go](meta/backend/internal/service/scan_service.go)
- Common client: [common/client/system.go](common/client/system.go)
- Common frontend basic: [common-frontend/basic/src/index.js](common-frontend/basic/src/index.js)
- Common frontend map: [common-frontend/map/src/index.js](common-frontend/map/src/index.js)

## Troubleshooting

**JWT token issues**: Ensure `JWT_SECRET` in `.env` matches across services (System and Gateway need same secret)
**Cross-service call failures**: Verify `ENABLE_SERVICE_INTEGRATION=true` and service URLs are correct in docker-compose.yml
