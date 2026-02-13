# Authorization Analysis Report

## 1. Executive Summary

- **Analysis Status:** Complete
- **Key Outcome:** 16 high-confidence authorization vulnerabilities identified across three modules (System, Service, Orchestrator). All findings represent complete tenant isolation bypasses enabling cross-tenant data access, modification, and denial of service. All vulnerabilities have been passed to the exploitation phase via the machine-readable exploitation queue.
- **Purpose of this Document:** This report provides the strategic context, dominant patterns, and architectural intelligence necessary to effectively exploit the vulnerabilities listed in the queue. It is intended to be read alongside the JSON deliverable.

**Vulnerability Breakdown:**
- **Horizontal IDOR:** 15 vulnerabilities across System (4), Service (8), and Orchestrator (3) modules
- **Context/Workflow:** 1 vulnerability in Orchestrator execution endpoint
- **Vertical Escalation:** 0 vulnerabilities (all privilege boundaries properly enforced)

**Impact Summary:**
- **Critical Findings:** Complete tenant isolation bypass in Service and Orchestrator modules
- **Attack Surface:** 16 endpoints allow authenticated users to access, modify, or delete resources belonging to other tenants
- **Data at Risk:** Application API keys, query service configurations, external service registrations, workflow orchestrations
- **Exploitation Complexity:** Low - simple ID enumeration enables full cross-tenant access

## 2. Dominant Vulnerability Patterns

### Pattern 1: Missing Tenant Isolation in Service Layer (Horizontal)
- **Description:** Service modules (Application, Service, Orchestrator) perform database operations using only resource ID without tenant_id validation. Handlers do not extract or pass tenant_id to service layer methods.
- **Implication:** Any authenticated user can access, modify, or delete resources across all tenants by enumerating sequential resource IDs
- **Representative Examples:** AUTHZ-VULN-01, AUTHZ-VULN-05, AUTHZ-VULN-13
- **Code Pattern:**
  ```go
  // VULNERABLE: Handler does not pass tenant_id
  func (h *ApplicationHandler) GetByID(c *gin.Context) {
      id := BindIDParam(c)
      app, err := h.service.GetByID(id)  // No tenant_id passed
      RespondOrError(c, app, err)
  }

  // VULNERABLE: Service fetches by ID only
  func (s *ApplicationService) GetByID(id uint) (*Application, error) {
      return s.repo.FindByID(id)  // No WHERE tenant_id clause
  }
  ```
- **Affected Modules:** System/Application (4 endpoints), Service/Query (3 endpoints), Service/Registered (5 endpoints), Orchestrator (4 endpoints)
- **Root Cause:** Service layer methods were designed without tenant context, likely developed before multi-tenancy was fully implemented

### Pattern 2: Incomplete JWT Context Extraction (Horizontal)
- **Description:** Orchestrator module has TODO comments indicating incomplete JWT parsing and tenant_id extraction in handlers
- **Implication:** Even though authentication middleware runs, handler code does not extract tenant_id from JWT context to pass to authorization checks
- **Representative Examples:** AUTHZ-VULN-13 (line 65 has TODO comment)
- **Code Evidence:**
  ```go
  // /Users/pampa/code/addp/orchestrator/backend/internal/api/handler.go:65
  // TODO: 从 JWT 中提取 tenant_id
  func (h *OrchestrationHandler) GetByID(c *gin.Context) {
      id := ParseIDParam(c)
      orch, err := h.orchRepo.GetByID(uint(id))  // No tenant check
      // ...
  }
  ```
- **Affected Module:** Orchestrator (all 4 endpoints)
- **Root Cause:** Development incomplete - authentication infrastructure exists but authorization layer not integrated

### Pattern 3: Missing Ownership Validation in Workflow Actions (Context)
- **Description:** Workflow execution endpoints do not verify requesting user owns the orchestration before triggering execution
- **Implication:** Users can execute workflows owned by other tenants, accessing their data sources and consuming their compute resources
- **Representative Example:** AUTHZ-VULN-16
- **Code Pattern:**
  ```go
  // VULNERABLE: No ownership check before execution
  func (h *OrchestrationHandler) Execute(c *gin.Context) {
      id := ParseIDParam(c)
      orch, err := h.orchRepo.GetByID(uint(id))  // Fetches any orchestration
      execution := &Execution{
          OrchestrationID: orch.ID,
          TenantID: orch.TenantID,  // Uses victim's tenant
      }
      h.execRepo.Create(execution)  // Creates execution
      h.executor.ExecuteAsync(execution.ID)  // Triggers workflow
  }
  ```
- **Affected Endpoint:** POST /api/orchestrator/orchestrations/:id/execute
- **Root Cause:** Workflow execution logic assumes caller owns orchestration, missing explicit authorization check

## 3. Strategic Intelligence for Exploitation

### Session Management Architecture:
- **JWT Token Structure:** Tokens contain `user_id`, `username`, `tenant_id`, `exp`, `iat` claims
- **Token Storage:** Frontend stores tokens in localStorage (accessible to JavaScript)
- **Token Validation:** Middleware validates JWT signature and expiration, extracts claims to context
- **Critical Finding:** Many handlers extract `user_id` but fail to extract or validate `tenant_id` for authorization

### Multi-Tenant Data Model:
- **Tenant Isolation Strategy:** Row-level `tenant_id` foreign key in most tables
- **SuperAdmin Exception:** SuperAdmin has `tenant_id = NULL` and bypasses tenant checks
- **Critical Finding:** Database queries in vulnerable modules lack `WHERE tenant_id = ?` clauses, relying solely on application-layer checks that are missing

### Resource Access Patterns:
- **ID Format:** Sequential unsigned integers (uint) starting from 1
- **Enumeration Attack:** Trivial to iterate IDs (1, 2, 3, ...) to discover cross-tenant resources
- **No RBAC on Resources:** Resources have `created_by` user_id but no granular ACLs - tenant ownership is the only boundary
- **Critical Finding:** Service and Orchestrator modules perform `db.First(&resource, id)` without tenant filtering

### API Key Authentication:
- **Application API Keys:** Users can create applications and generate API keys for authentication
- **AUTHZ-VULN-02 Impact:** Attackers can generate API keys for victim tenants' applications
- **Attack Chain:** Discover victim app ID → Generate API key → Authenticate as victim → Full tenant access
- **Critical Finding:** API key generation endpoint lacks tenant boundary check, enabling complete tenant takeover

### Service Module Architecture:
- **Query Services:** Expose database tables/views as HTTP endpoints with configurable filters
- **Registered Services:** Proxy to external OGC/WMS/WMTS services with stored credentials
- **Critical Finding:** All 8 Service module endpoints lack tenant isolation, exposing data publication configurations and external service URLs/credentials

### Orchestrator Module Architecture:
- **Workflow Orchestrations:** DAG-based workflows connecting to Python/Jupyter/Spark engines
- **Execution Model:** POST to /execute creates execution record and triggers async execution
- **Engine Configuration:** Workflows reference database engines containing connection credentials
- **Critical Finding:** Missing JWT extraction (TODO comments) means all orchestration operations bypass tenant checks
- **Execution Risk:** AUTHZ-VULN-16 allows cross-tenant workflow execution, potentially accessing victim's data sources and consuming compute resources

### Error Message Behavior:
- **404 vs 403 Pattern:** Most vulnerable endpoints return 404 "resource not found" even when ID exists but belongs to different tenant
- **Exploitation Advantage:** 200 response = resource exists and accessed successfully, 404 = resource doesn't exist OR authorization failed (no information leakage)
- **Attack Strategy:** Enumerate IDs until 200 response confirms successful cross-tenant access

## 4. Vectors Analyzed and Confirmed Secure

These authorization checks were traced and confirmed to have robust, properly-placed guards. They are **low-priority** for further testing.

| **Endpoint** | **Guard Location** | **Defense Mechanism** | **Verdict** |
|--------------|-------------------|----------------------|-------------|
| `GET /api/system/users/:id` | user_service.go:104-120 | Tenant isolation check (line 111) + ownership validation (line 118) before returning data | SAFE |
| `PUT /api/system/users/:id` | user_service.go:162-233 | Multi-layer validation: validateUpdatePermission checks ownership (line 223), validateCreatePermission blocks role escalation (line 82-84) | SAFE |
| `DELETE /api/system/users/:id` | user_service.go:254-271 | SuperAdmin/TenantAdmin check before deletion, tenant admins cannot delete other tenant admins (line 263-266) | SAFE |
| `PUT /api/system/users/:id/change-password` | user_service.go:328-330 | Self-only check (line 328) before any database access, even admins cannot change other passwords via this endpoint | SAFE |
| `GET /api/system/engines/:id` | engine_service.go:178 | authorizeResourceAccess() validates tenant before returning (credentials masked with ******) | SAFE |
| `PUT /api/system/engines/:id` | engine_service.go:234-238 | Double authorization: tenant check + ensureResourceManagementPermission (admin-only) before update | SAFE |
| `DELETE /api/system/engines/:id` | engine_service.go:310-316 | Tenant check + admin check + builtin protection (line 319) before deletion | SAFE |
| `POST /api/system/engines/:id/test` | engine_service.go:416-420 | Tenant check + admin check before decrypting credentials and testing connection | SAFE |
| `GET /api/system/engines/:id/schemas` | engine_handler.go:323 | Inherits authorization from GetForConnection (tenant + admin checks) before listing schemas | SAFE |
| `GET /api/system/engines/:id/tables` | engine_handler.go:360 | Same authorization pattern as schemas endpoint (tenant + admin checks) | SAFE |
| `POST /api/system/users` | user_service.go:74-90 | validateCreatePermission ensures TenantAdmin can only create regular users (line 82-84) before user creation | SAFE |
| `PUT /api/system/users/:id` (role change) | user_service.go:182-192 | Only admins can modify user_type (line 182), validateCreatePermission blocks unauthorized role assignments (line 188) | SAFE |
| `POST /api/system/engines` | engine_service.go:50-52 | ensureResourceManagementPermission blocks non-admin users before engine creation at line 102 | SAFE |
| `POST /api/system/tenants` | tenant_service.go:33-35 | Explicit SuperAdmin check (UserType == UserTypeSuperAdmin) before tenant creation at line 59 | SAFE |
| `GET /api/system/tenants` | tenant_service.go:115-117 | SuperAdmin-only check before listing tenants at line 120 | SAFE |
| `PUT /api/system/tenants/:id` | tenant_service.go:130-132 | SuperAdmin-only check before tenant modification at line 156 | SAFE |
| `DELETE /api/system/tenants/:id` | tenant_service.go:170-172 | SuperAdmin-only check before tenant deletion transaction at line 181 | SAFE |
| `GET /api/transfer/tasks/:id` | task_service.go:138 | Validates `task.TenantID != tenantID` before returning data | SAFE |
| `PUT /api/transfer/tasks/:id` | task_service.go:146 | Calls GetTask() which validates tenant ownership first | SAFE |
| `DELETE /api/transfer/tasks/:id` | task_service.go:215 | Calls GetTask() to verify ownership before deletion | SAFE |
| `POST /api/transfer/tasks/:id/start` | task_service.go:273-284 | Transaction locking with tenant validation (line 273) + state check (line 282) before execution creation | SAFE |
| `POST /api/transfer/tasks/:id/stop` | task_service.go:348-355 | GetTask validates ownership (line 348) + state check (line 353) before stopping execution | SAFE |
| `GET /api/transfer/executions/:id` | execution_handler.go:25 | Service layer validates tenant via GetExecution(tenantID) | SAFE |
| `GET /api/develop/items/:id` | dev_item_service.go:149 | Repository layer filters by tenant: FindByID(id, tenantID) | SAFE |
| `PUT /api/develop/items/:id` | dev_item_service.go:92 | GetDevItem(id, tenantID) validates ownership first | SAFE |
| `DELETE /api/develop/items/:id` | dev_item_service.go:176 | GetDevItem(id, tenantID) verifies ownership before deletion | SAFE |
| `POST /api/develop/items/:id/execute` | dev_execution_handler.go:33 | Executor receives tenantID and validates ownership before execution | SAFE |

**Common Secure Patterns Observed:**
1. **System/Engine Module:** Implements `authorizeResourceAccess()` and `ensureResourceManagementPermission()` helpers for consistent tenant + role checks
2. **Transfer Module:** Consistently uses `GetTask(id, tenantID)` helper that validates ownership before all operations
3. **Develop Module:** Repository layer enforces tenant filtering in all queries
4. **User Management:** Multi-layer validation with separate functions for create/update permissions
5. **Privilege Escalation:** All vertical escalation paths properly blocked with explicit SuperAdmin/TenantAdmin checks before side effects

**Key Takeaway:** The secure endpoints demonstrate that the development team understands authorization principles. The vulnerable modules (Application, Service, Orchestrator) appear to be outliers where authorization integration was incomplete or overlooked.

## 5. Analysis Constraints and Blind Spots

### Unanalyzed Components:

**Internal API Endpoints (`/api/internal/*`):**
- **Reason for Exclusion:** Based on external attacker scope requirement, internal APIs requiring `X-Internal-API-Key` header and likely restricted to Docker internal network were not included in final vulnerability count
- **Potential Risk:** If internal APIs are exposed through reverse proxy or API key is compromised, these endpoints bypass all tenant isolation
- **Note:** Reconnaissance report indicates weak internal API key validation ("dev-internal-key" default value)

**Python Workflow Engines (Ports 8099, 8199, 8299):**
- **Authorization Model:** These engines receive execution requests from Orchestrator service
- **Blind Spot:** Could not analyze authorization within workflow engine codebases (Python Flask applications)
- **Assumption:** Engines trust tenant_id from Orchestrator service, but if engines are directly accessible, tenant isolation may be bypassable

**Frontend Authorization (Vue.js Portal):**
- **Analysis Scope:** Only analyzed backend API authorization guards
- **Blind Spot:** Did not verify if frontend hides UI elements based on roles (defense in depth)
- **Note:** UI-only checks are not security boundaries per methodology, but their presence/absence affects user experience

### Dynamic Permission Systems:

**Engine Capabilities:**
- **Observation:** Engine records have `capabilities` JSON field that may enable/disable features
- **Blind Spot:** Could not determine if runtime capability checks exist beyond database storage
- **Impact:** If capabilities enforce additional authorization, secure endpoints may have defense-in-depth not analyzed

**Application Rate Limiting:**
- **Observation:** Applications have `rate_limit` and `daily_quota` fields
- **Blind Spot:** Did not analyze if rate limiting enforcement provides any authorization-like protection
- **Impact:** Rate limits may slow but not prevent exploitation of authorization bypasses

### Concurrency and Race Conditions:

**State Transitions:**
- **Analysis Focus:** Verified guards exist before state changes, but did not analyze race condition resilience
- **Example:** Transfer task start/stop endpoints use transaction locking (`FOR UPDATE`), but other endpoints may have TOCTOU vulnerabilities
- **Impact:** Some endpoints may be vulnerable to race conditions even if single-threaded authorization is correct

### GraphQL Endpoints:

**Scope:** Reconnaissance report mentions 300+ endpoints but analysis focused on REST APIs documented in section 4
- **Blind Spot:** If GraphQL endpoints exist, their authorization model was not analyzed
- **Recommendation:** Verify if GraphQL resolvers implement same tenant isolation as REST handlers

### SuperAdmin Behavior:

**Analysis Decision:** SuperAdmin bypassing tenant checks is **expected behavior** per codebase design
- **Not Reported:** SuperAdmin accessing all tenants is **NOT** a vulnerability
- **Validation:** Confirmed SuperAdmin checks occur before privileged operations (tenant creation/deletion)
- **Blind Spot:** Did not verify if SuperAdmin actions are logged for audit (accountability without authorization)

