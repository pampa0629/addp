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
