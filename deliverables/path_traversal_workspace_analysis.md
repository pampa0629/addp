# Path Traversal Vulnerability Analysis: Workspace File Serving

## Vulnerability Summary

**Vulnerability ID:** PATH-TRAVERSAL-01
**Severity:** NOT EXPLOITABLE (Lab-Only Component)
**Confidence:** HIGH
**Status:** NOT EXTERNALLY ACCESSIBLE

**Entry Point:** `GET /api/workspace/*filepath`
**File:** `/Users/pampa/code/addp/labs/abs/backend/internal/api/handler.go:194-220`
**Reported Issue:** "Only checks if file exists, no path canonicalization"

**Verdict:** **NOT EXTERNALLY EXPLOITABLE** - The ABS (AI-Bootstrapping System) backend is a standalone development/lab service that is NOT part of the main ADDP deployment and cannot be accessed from the target application at http://localhost:5170.

---

## 1. External Accessibility Analysis

### 1.1 Deployment Status

**The ABS backend is NOT deployed with the main ADDP application.**

**Evidence:**

1. **No Docker Compose Integration:**
   - The main ADDP deployment is defined in `/Users/pampa/code/addp/docker-compose.yml`
   - Searching the main docker-compose file shows NO mention of "abs" service
   - The ABS directory `/Users/pampa/code/addp/labs/abs/` has NO `docker-compose.yml` file
   - ABS is located in the `/labs/` directory, which contains experimental/standalone projects

2. **ABS is a Standalone Development Tool:**
   - **Purpose:** AI-powered code generation and auto-deployment system (per README.md)
   - **Intended Use:** Local development tool for generating code using Codex/Claude APIs
   - **Ports:** Backend runs on port 8090, Frontend on port 5180
   - **Manual Startup:** Requires explicit `./restart.sh` or `make dev` commands from `/labs/abs/` directory

3. **Target Application Ports:**
   - **Main ADDP:** http://localhost:5170 (Portal frontend)
   - **ABS (if running):** http://localhost:5180 (ABS frontend), http://localhost:8090 (ABS backend)
   - These are **completely separate applications** on different ports

4. **Gateway Routing Configuration:**
   - The ADDP Gateway service (port 8000) has NO routes configured for `/api/workspace/*`
   - Gateway only proxies to: System, Manager, Meta, Transfer, Orchestrator, Develop, Service, Copilot services
   - No proxy configuration exists to forward requests to ABS backend on port 8090

### 1.2 Network Accessibility

**From the target application (http://localhost:5170):**

```
Client Request: GET http://localhost:5170/api/workspace/*filepath
  ↓
Portal Frontend (Nginx on port 5170)
  ↓ proxies /api/* to
Gateway (port 8000)
  ↓
Gateway attempts to route /api/workspace/*
  ↓
❌ NO ROUTE FOUND - 404 Not Found (or routed to wrong service)
```

**The ABS backend is NOT reachable through this flow.**

**If ABS were running independently:**

```
Client Request: GET http://localhost:8090/api/workspace/*filepath
  ↓ (requires direct connection to ABS backend, separate from main app)
ABS Backend (port 8090)
  ↓
handler.go:ServeWorkspaceFile()
  ↓
Vulnerable path traversal code
```

**However, this requires:**
- ABS backend to be manually started (not part of main deployment)
- Direct access to port 8090 (separate from main application on port 5170)
- Attacker to know about the existence of this separate development service

### 1.3 Conclusion: Not Externally Accessible

**The vulnerability is NOT exploitable against the target application because:**

1. ✅ ABS is NOT included in the main ADDP docker-compose deployment
2. ✅ ABS runs on different ports (8090/5180) from the main application (5170/8000)
3. ✅ The ADDP Gateway has NO routing rules to forward requests to ABS
4. ✅ ABS is located in `/labs/` directory, indicating it's an experimental/development tool
5. ✅ ABS requires manual startup and is NOT launched by the main deployment scripts

**This is similar to reporting a vulnerability in a developer's local VS Code extension - it exists in the codebase but is not part of the deployed application's attack surface.**

---

## 2. Vulnerability Technical Analysis (For Completeness)

Although this endpoint is not externally accessible, documenting the vulnerability for completeness:

### 2.1 Data Flow

**Handler Implementation** (`handler.go:192-220`):

```go
// ServeWorkspaceFile 提供 workspace 文件服务 (用于前端 iframe)
func ServeWorkspaceFile(workspaceDir string) gin.HandlerFunc {
    return func(c *gin.Context) {
        filepath := c.Param("filepath")  // ← USER INPUT (includes leading slash)

        // 拼接时确保没有双斜杠
        fullPath := workspaceDir + filepath  // ← DIRECT CONCATENATION, NO VALIDATION

        // Debug logs
        log.Printf("[DEBUG] ServeWorkspaceFile - workspaceDir: %s", workspaceDir)
        log.Printf("[DEBUG] ServeWorkspaceFile - filepath param: %s", filepath)
        log.Printf("[DEBUG] ServeWorkspaceFile - fullPath: %s", fullPath)

        // 检查文件是否存在
        info, err := os.Stat(fullPath)  // ← Only checks existence, no path validation
        if err != nil {
            log.Printf("[ERROR] File stat error: %v", err)
            c.String(http.StatusNotFound, "File not found")
            return
        }

        // 如果是目录，尝试查找 index.html
        if info.IsDir() {
            fullPath = fullPath + "/index.html"
            log.Printf("[DEBUG] IsDir, trying index.html: %s", fullPath)
        }

        // 使用 http.ServeFile 避免 301 重定向问题
        http.ServeFile(c.Writer, c.Request, fullPath)
    }
}
```

**Router Registration** (`router.go:49` and `router.go:59`):

```go
// Registered twice (with and without /api prefix)
api.GET("/workspace/*filepath", ServeWorkspaceFile(config.WorkspaceDir))
router.GET("/workspace/*filepath", ServeWorkspaceFile(config.WorkspaceDir))
```

### 2.2 Configuration

**WorkspaceDir Configuration** (`config.go:42-49`):

```go
// Default to ../workspace (parent directory) since backend runs from backend/ dir
workspaceDir := getEnv("WORKSPACE_DIR", "../workspace")
absWorkspaceDir, err := filepath.Abs(workspaceDir)
if err != nil {
    // Fallback to relative path if conversion fails
    absWorkspaceDir = workspaceDir
}
```

**Default workspace directory:** `/Users/pampa/code/addp/labs/abs/workspace/` (absolute path)

### 2.3 Vulnerability Details

**Issue:** Path traversal via directory traversal sequences (`../`)

**Root Cause:**
1. ❌ User-controlled `filepath` parameter concatenated directly with `workspaceDir`
2. ❌ NO path canonicalization (e.g., `filepath.Clean()`)
3. ❌ NO check to verify final path is within `workspaceDir` boundary
4. ❌ NO validation to reject `..` sequences
5. ✅ Only validates file existence with `os.Stat()` (insufficient)

**Exploitation Path (if endpoint were accessible):**

```http
GET /api/workspace/../../../etc/passwd HTTP/1.1
Host: localhost:8090
```

**Data Flow:**
```
filepath = "/../../../etc/passwd"  (from URL parameter)
workspaceDir = "/Users/pampa/code/addp/labs/abs/workspace"
fullPath = workspaceDir + filepath
         = "/Users/pampa/code/addp/labs/abs/workspace/../../../etc/passwd"
         = "/Users/pampa/code/addp/labs/etc/passwd"  (simplified by OS)
os.Stat(fullPath) → succeeds if file exists
http.ServeFile() → returns file contents
```

**Result:** Arbitrary file read outside workspace directory

### 2.4 Impact Assessment (If Exploitable)

**If this endpoint were accessible, the impact would be HIGH:**

**Sensitive Files Readable:**
- `/etc/passwd` - System user information
- `/Users/pampa/code/addp/.env` - Environment variables with API keys
- `/Users/pampa/.codex/.apikey` - Codex API key (mentioned in ABS README)
- `/Users/pampa/code/addp/labs/abs/backend/.env` - ABS configuration
- Application source code
- Database configuration files

**Example Payloads:**
```http
# Read environment file (API keys)
GET /api/workspace/../../../../.env

# Read Codex API key
GET /api/workspace/../../../../../.codex/.apikey

# Read application source code
GET /api/workspace/../backend/internal/service/claude_client.go

# Read system files
GET /api/workspace/../../../etc/passwd
```

**Limitations:**
- ✅ Can only read files (not write/execute)
- ✅ Requires target file to exist (404 if not found)
- ✅ Requires target file to be readable by ABS process user
- ✅ Binary files served as-is (may need decoding)

---

## 3. Security Posture

### 3.1 Why This is NOT a Real-World Threat

**The vulnerability exists in code but is not exploitable because:**

1. **Scope Boundary:** ABS is a development tool, not part of the production application
2. **Deployment Isolation:** ABS is never deployed with the main ADDP stack
3. **Port Separation:** ABS runs on port 8090, main app on 5170/8000 (no overlap)
4. **No Gateway Routing:** ADDP Gateway cannot forward requests to ABS backend
5. **Manual Startup Required:** ABS requires explicit developer action to run

**Analogy:** This is like finding a vulnerability in a developer's local Docker Compose file used for testing - it exists in the repository but is not part of the deployed application's attack surface.

### 3.2 Recommended Fix (For Lab Security)

**Even though this is not externally exploitable, the code should still be fixed to prevent local security issues:**

**Secure Implementation:**

```go
func ServeWorkspaceFile(workspaceDir string) gin.HandlerFunc {
    // Convert to absolute path once at startup
    absWorkspaceDir, err := filepath.Abs(workspaceDir)
    if err != nil {
        log.Fatalf("Failed to resolve workspace directory: %v", err)
    }

    return func(c *gin.Context) {
        filepath := c.Param("filepath")

        // Clean the filepath to remove .. sequences and normalize path
        cleanPath := filepath.Clean(filepath)

        // Prevent absolute paths
        if filepath.IsAbs(cleanPath) {
            c.String(http.StatusForbidden, "Absolute paths not allowed")
            return
        }

        // Join and clean again to resolve final path
        fullPath := filepath.Join(absWorkspaceDir, cleanPath)

        // Canonicalize to resolve symlinks and .. sequences
        canonicalPath, err := filepath.Abs(fullPath)
        if err != nil {
            c.String(http.StatusInternalServerError, "Path resolution error")
            return
        }

        // Ensure final path is within workspace boundary
        if !strings.HasPrefix(canonicalPath, absWorkspaceDir) {
            log.Printf("[SECURITY] Path traversal attempt blocked: %s", filepath)
            c.String(http.StatusForbidden, "Access denied")
            return
        }

        // Check if file exists
        info, err := os.Stat(canonicalPath)
        if err != nil {
            c.String(http.StatusNotFound, "File not found")
            return
        }

        // If directory, serve index.html
        if info.IsDir() {
            canonicalPath = filepath.Join(canonicalPath, "index.html")
        }

        // Serve file
        http.ServeFile(c.Writer, c.Request, canonicalPath)
    }
}
```

**Key Security Improvements:**
1. ✅ Use `filepath.Clean()` to normalize path and remove `..` sequences
2. ✅ Use `filepath.Join()` for safe path concatenation
3. ✅ Use `filepath.Abs()` to resolve canonical path
4. ✅ Validate final path starts with `workspaceDir` (boundary check)
5. ✅ Reject absolute paths in user input
6. ✅ Log security-relevant rejections for monitoring

---

## 4. Comparison with Other Lab Findings

**This is consistent with our previous analysis of the ABS backend:**

### 4.1 Command Injection in ABS (Also Not Exploitable)

**Previous Finding:** Command injection vulnerability in `POST /api/apps/:id/launch`
- **Severity:** CRITICAL (if accessible)
- **Status:** NOT EXTERNALLY EXPLOITABLE
- **Reason:** Same - ABS backend not part of main deployment

**Same Pattern:**
- Both vulnerabilities exist in `/labs/abs/backend/`
- Both are real code vulnerabilities (would be exploitable if deployed)
- Both are NOT accessible from http://localhost:5170
- Both require manual startup of ABS service on separate ports

### 4.2 Lab Directory Context

**Other labs in `/labs/` directory:**
- `/labs/mvt/` - Map Vector Tiles testing
- `/labs/dolphin/` - Dolphin lab project
- `/labs/vector/` - Vector processing experiments
- `/labs/doris_test/` - Apache Doris testing
- `/labs/orch/` - Orchestration experiments

**All of these are development/testing environments, not production services.**

---

## 5. Penetration Test Scope Ruling

### 5.1 Scope Definition (from Code Analysis Deliverable)

**In-Scope:** Components that can be invoked through the running application's network interface at http://localhost:5170

**Out-of-Scope:** Components that require execution context external to the application's request-response cycle, including:
- Local development servers
- Test harnesses
- Debugging utilities
- Tools that must be run via command-line interface

### 5.2 Ruling: OUT OF SCOPE

**The ABS workspace file serving endpoint is OUT OF SCOPE because:**

1. ✅ It requires manual startup of ABS service (`./restart.sh` or `make dev`)
2. ✅ It runs on a separate port (8090) not exposed by the main application
3. ✅ It is located in `/labs/` directory (experimental/development tools)
4. ✅ It is NOT included in the main deployment (docker-compose.yml)
5. ✅ It cannot be reached through the application's network interface (http://localhost:5170)

**This matches the exact criteria for "local development servers" and "debugging utilities" that are explicitly listed as out-of-scope.**

---

## 6. Final Determination

### 6.1 Vulnerability Status

**External Exploitability:** ❌ NOT EXPLOITABLE
**Code Quality Issue:** ✅ YES (should be fixed for local security)
**Production Risk:** ✅ NONE (not deployed)
**Penetration Test Scope:** ❌ OUT OF SCOPE

### 6.2 Recommendation

**For the penetration test report:**
- ❌ Do NOT include as an exploitable vulnerability
- ❌ Do NOT assign a severity rating for production risk
- ✅ MAY mention in "Code Quality Observations" section (optional)
- ✅ Focus testing efforts on actual deployed endpoints

**For the development team:**
- ✅ Fix the path traversal vulnerability in ABS (for local security)
- ✅ Review other `/labs/` code for similar issues
- ✅ Ensure labs are never accidentally deployed to production
- ✅ Consider adding `.dockerignore` to exclude `/labs/` from production builds

### 6.3 Rationale

**This is NOT a false positive - the vulnerability is real in the code.**

**However, it is NOT exploitable because:**
- The vulnerable service is not deployed
- The vulnerable service is not accessible from the target application
- The vulnerable service requires manual startup on different ports
- The vulnerable service is clearly marked as a development tool (`/labs/`)

**Testing this endpoint would be like:**
- Installing a vulnerable VS Code extension and reporting it as an application vulnerability
- Finding a bug in a local test script and claiming it's a production exploit
- Discovering an issue in docker-compose.dev.yml and reporting it as a production risk

**None of these scenarios represent real attack surface against the deployed application.**

---

## 7. Summary

**Finding:** Path Traversal vulnerability exists in ABS workspace file serving endpoint
**Location:** `/Users/pampa/code/addp/labs/abs/backend/internal/api/handler.go:194-220`
**Endpoint:** `GET /api/workspace/*filepath`
**Target Application:** http://localhost:5170 (ADDP Portal)

**Accessibility:** ❌ NOT ACCESSIBLE from target application
**Reason:** ABS backend is a standalone lab/development service not deployed with main ADDP application
**Deployment Status:** Not in docker-compose.yml, requires manual startup on different ports (8090/5180)
**Scope Ruling:** OUT OF SCOPE (local development tool, not production service)

**Recommended Action:** Document as "Code Quality Issue - Lab Service" but do NOT report as exploitable vulnerability in penetration test findings.

---

**Analysis completed on:** 2026-02-13
**Analyst:** Security Research Team
**Confidence Level:** HIGH (confirmed via deployment configuration analysis)
