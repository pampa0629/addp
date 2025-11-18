# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Overview

**DolphinScheduler Learning Lab** is an experimental project within the ADDP platform's labs directory for exploring and understanding Apache DolphinScheduler (海豚调度器) - a distributed and extensible workflow scheduler platform.

**Location**: `/Users/pampa/code/addp/labs/dolphin`

**Status**: **NEWLY INITIALIZED** - No implementation yet, ready for development

**Purpose**: Learn and experiment with DolphinScheduler's capabilities in isolation from the main ADDP system to avoid complexity overhead.

## What is Apache DolphinScheduler?

Apache DolphinScheduler is a distributed workflow scheduler system that features:
- DAG (Directed Acyclic Graph) workflow visualization
- Task dependency management
- Multi-tenancy and resource management
- Support for multiple task types (Shell, SQL, Spark, Python, etc.)
- High availability and scalability
- RESTful API for integration

## Project Context

This is part of the `labs/` directory in the ADDP (All Domain Data Platform) project:
- **Labs Purpose**: Rapid prototyping and feature research
- **Isolation**: Each lab is independent - no interaction with main ADDP system
- **Goal**: Learn quickly without architectural complexity

**Sibling Labs**:
- `mvt/` - MapBox Vector Tiles rendering system (Go + Vue, PostgreSQL + Redis)
- `abs/` - Abstract storage system (established)

## Expected Technology Stack

Based on ADDP platform patterns, when implemented this lab will likely use:

### Backend
- **Language**: Go 1.23+ (following ADDP convention)
- **HTTP Framework**: Gin (standard across ADDP services)
- **Database**: PostgreSQL 15 (if persistence needed)
- **Cache/Queue**: Redis 7 (for distributed task scheduling)
- **DolphinScheduler**: Docker deployment or API integration

### Frontend (Optional)
- **Framework**: Vue 3 + Composition API
- **Build Tool**: Vite
- **UI Library**: Element Plus (ADDP standard)
- **Mapping**: Axios for HTTP client

### Infrastructure
- **Containerization**: Docker Compose
- **Scheduler**: Apache DolphinScheduler (standalone or cluster mode)

## Typical Development Workflow (When Implemented)

### Initial Setup
```bash
# Create project structure
mkdir -p backend/{cmd/server,internal/{api,service,models},config}
mkdir -p frontend/src/{views,components,api}

# Initialize Go module
cd backend
go mod init github.com/addp/labs/dolphin/backend

# Initialize Vue frontend
cd frontend
npm create vite@latest . -- --template vue
npm install
```

### Expected Make Commands (Future)
Based on `mvt/` lab pattern:

```bash
# First-time setup
make init           # Install dependencies + create config files

# Start infrastructure
make up             # Start Docker services (DolphinScheduler + Redis)

# Development
make dev            # Run backend + frontend concurrently
make dev-backend    # Backend only (Go live reload)
make dev-frontend   # Frontend only (Vite HMR)

# Database operations
make db-shell       # Connect to PostgreSQL
make redis-cli      # Connect to Redis

# Production build
make build          # Build backend binary + frontend static files

# Cleanup
make down           # Stop Docker services
make clean          # Remove temporary files
```

### Expected Directory Structure (Future)
```
dolphin/
├── backend/
│   ├── cmd/
│   │   └── server/
│   │       └── main.go              # Application entry point
│   ├── internal/
│   │   ├── api/                     # HTTP handlers
│   │   ├── service/                 # Business logic (DolphinScheduler integration)
│   │   ├── models/                  # Data structures
│   │   └── config/                  # Configuration loader
│   ├── go.mod
│   └── go.sum
├── frontend/                         # Vue 3 frontend (optional)
│   ├── src/
│   │   ├── views/                   # Workflow visualization pages
│   │   ├── components/              # Reusable UI components
│   │   └── api/                     # API client for backend
│   ├── package.json
│   └── vite.config.js
├── docker-compose.yml               # DolphinScheduler + dependencies
├── Makefile                          # Build and run commands
├── .env.example                      # Configuration template
├── .gitignore
├── readme.md                         # Chinese description
└── CLAUDE.md                         # This file
```

## Development Focus Areas

When implementing this lab, focus on learning:

1. **DolphinScheduler Architecture**
   - Standalone vs cluster deployment
   - Master/Worker/API server components
   - Database schema and metadata management

2. **API Integration**
   - RESTful API authentication
   - Workflow definition (DAG creation)
   - Task scheduling and execution
   - Log retrieval and monitoring

3. **Task Types**
   - Shell script execution
   - SQL task execution (integrate with ADDP data sources)
   - HTTP task for API calls
   - Python/Spark tasks (optional)

4. **Integration Patterns**
   - How to trigger DolphinScheduler workflows from Go backend
   - How to monitor workflow execution status
   - How to handle task failures and retries
   - How to pass parameters between tasks

5. **Visualization**
   - Display workflow DAGs in Vue frontend
   - Show task execution history
   - Real-time log streaming
   - Task dependency visualization

## Configuration

### Expected Environment Variables
```bash
# DolphinScheduler Connection
DOLPHIN_API_URL=http://localhost:12345/dolphinscheduler
DOLPHIN_TOKEN=your-api-token

# PostgreSQL (optional, for metadata storage)
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_DB=dolphin_lab
POSTGRES_USER=dolphin
POSTGRES_PASSWORD=password

# Redis (for distributed locking)
REDIS_HOST=localhost
REDIS_PORT=6379

# Service Configuration
PORT=8093                             # Backend port
FRONTEND_PORT=5177                    # Frontend dev port
```

### Expected Port Assignments
Following ADDP labs pattern:
- **Backend**: 8093 (next available after mvt:8090, abs:8091, etc.)
- **Frontend**: 5177 (dev mode, next available after mvt:5180)
- **DolphinScheduler API**: 12345 (default)
- **DolphinScheduler UI**: 12346 (default web interface)

## Testing Strategy

### Backend Testing (Go)
```bash
# Run all tests
cd backend && go test ./...

# Run with coverage
go test -cover ./...

# Run specific package
go test ./internal/service/...

# Verbose output
go test -v ./...
```

### Integration Testing
- Test DolphinScheduler API connectivity
- Test workflow creation and execution
- Test task parameter passing
- Test error handling and retries

## Common Development Tasks

### Starting DolphinScheduler with Docker
```bash
# Pull official image
docker pull apache/dolphinscheduler:latest

# Start standalone mode
docker-compose up -d

# Check logs
docker-compose logs -f dolphinscheduler
```

### Interacting with DolphinScheduler API
```bash
# Login and get token
curl -X POST http://localhost:12345/dolphinscheduler/login \
  -d "userName=admin&userPassword=dolphinscheduler123"

# Create project
curl -X POST http://localhost:12345/dolphinscheduler/projects \
  -H "token: your-token" \
  -d "projectName=test&description=Test Project"

# List projects
curl -X GET http://localhost:12345/dolphinscheduler/projects/list \
  -H "token: your-token"
```

## Learning Resources

When developing this lab, refer to:

1. **Official Documentation**
   - https://dolphinscheduler.apache.org/
   - https://dolphinscheduler.apache.org/en-us/docs/latest/user_doc/guide/start.html

2. **API Documentation**
   - Swagger UI at http://localhost:12345/dolphinscheduler/swagger-ui/
   - API reference for workflow creation and management

3. **ADDP Platform Patterns**
   - `/Users/pampa/code/addp/CLAUDE.md` - Main platform architecture
   - `/Users/pampa/code/addp/labs/mvt/CLAUDE.md` - Reference lab structure
   - Common module patterns for Go services

## Key Design Principles

Following ADDP labs standards:

1. **Simplicity**: Keep implementation minimal - focus on learning DolphinScheduler
2. **Isolation**: No dependencies on main ADDP system
3. **Experimentation**: Quick iterations without breaking production code
4. **Documentation**: Document learnings and patterns discovered
5. **DRY**: Extract common patterns if they emerge

## Next Steps

To start implementing this lab:

1. **Research Phase**
   - Read DolphinScheduler documentation
   - Deploy DolphinScheduler standalone via Docker
   - Explore Web UI and understand concepts

2. **Setup Phase**
   ```bash
   # Create Makefile
   touch Makefile

   # Create docker-compose.yml
   # Include: DolphinScheduler, PostgreSQL (optional), Redis

   # Initialize backend
   mkdir -p backend/cmd/server
   cd backend && go mod init github.com/addp/labs/dolphin/backend

   # Create .env.example
   touch .env.example
   ```

3. **Implementation Phase**
   - Build Go API client for DolphinScheduler
   - Create REST endpoints for workflow management
   - Add Vue frontend for visualization (optional)
   - Test workflow execution and monitoring

4. **Documentation Phase**
   - Document API patterns discovered
   - Create examples for common use cases
   - Update CLAUDE.md with actual implementation details

## Quick Start (Fastest Way to Learn)

**立即开始体验 DolphinScheduler**:

```bash
# 1. 启动 DolphinScheduler 容器
make start

# 2. 等待 1-2 分钟后打开 Web UI
make web
# 或手动访问: http://localhost:12345/dolphinscheduler/ui

# 3. 登录
#    用户名: admin
#    密码: dolphinscheduler123

# 4. 查看实时日志
make logs
```

**学习建议**: 参考 [QUICKSTART.md](QUICKSTART.md) 获取详细的分步学习指南。

## Current Status

**Files Present**:
- [readme.md](readme.md) - Chinese description of project purpose
- [CLAUDE.md](CLAUDE.md) - This file (development guide)
- [QUICKSTART.md](QUICKSTART.md) - Step-by-step learning guide
- [docker-compose.yml](docker-compose.yml) - DolphinScheduler standalone deployment
- [Makefile](Makefile) - Common commands for managing DolphinScheduler
- `.claude/settings.local.json` - Claude Code permissions config
- `.gitignore` - Git ignore patterns

**Infrastructure Ready** - DolphinScheduler can be started with `make start`

**Next Steps** - Complete Phase 1-2 in QUICKSTART.md, then implement Go client
