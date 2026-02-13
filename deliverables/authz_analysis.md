# ADDP Authorization Architecture Analysis

**Analysis Date:** 2026-02-13  
**Target Application:** All Domain Data Platform (ADDP)  
**Scope:** Network-accessible web application endpoints

---

## Executive Summary

This document provides a comprehensive analysis of the ADDP application's authorization architecture, including role definitions, permission models, authorization decision points, and object ownership patterns. The application implements a multi-tenant role-based access control (RBAC) system with three distinct user roles and tenant-based isolation.

---

## 1. User Roles

### 1.1 Role Definitions

The ADDP application defines three distinct user roles with hierarchical privileges:

| Role | Code Constant | Privilege Level | Description |
|------|--------------|-----------------|-------------|
| **SuperAdmin** | `UserTypeSuperAdmin = "super_admin"` | Highest | Platform-wide administrator with no tenant restrictions |
| **TenantAdmin** | `UserTypeTenantAdmin = "tenant_admin"` | Medium | Tenant-level administrator with full access to tenant resources |
| **User** | `UserTypeUser = "user"` | Lowest | Regular user with restricted access to owned resources |

**Location:** `/Users/pampa/code/addp/common/models/user.go:9-12`

```go
const (
    UserTypeSuperAdmin  UserType = "super_admin"  // 超级管理员
    UserTypeTenantAdmin UserType = "tenant_admin" // 租户管理员
    UserTypeUser        UserType = "user"         // 普通用户
)
```

### 1.2 User Model Structure

**Location:** `/Users/pampa/code/addp/common/models/user.go:15-27`

```go
type User struct {
    ID           uint      `gorm:"primaryKey" json:"id"`
    Username     string    `gorm:"not null;unique" json:"username"`
    Email        string    `gorm:"unique" json:"email"`
    PasswordHash string    `gorm:"not null" json:"-"`
    FullName     string    `json:"full_name"`
    IsActive     bool      `gorm:"default:true" json:"is_active"`
    UserType     UserType  `gorm:"type:varchar(20);default:'user';not null" json:"user_type"`
    TenantID     *uint     `gorm:"index" json:"tenant_id"` // SuperAdmin has nil tenant_id
    IsSuperuser  bool      `gorm:"default:false" json:"is_superuser"` // Legacy field
    CreatedAt    time.Time `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt    time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
```

**Key Authorization Fields:**
- `UserType`: Defines the role (super_admin, tenant_admin, or user)
- `TenantID`: Foreign key for tenant isolation (NULL for SuperAdmin)
- `IsActive`: Controls whether user can authenticate

---

## 2. Role Hierarchy & Privilege Ordering

### 2.1 Privilege Dominance

```
SuperAdmin (tenant_id = NULL)
    ↓ Can access all tenants
TenantAdmin (tenant_id = N)
    ↓ Can access all resources in tenant N
User (tenant_id = N)
    ↓ Can only access owned resources in tenant N
```

### 2.2 Access Patterns by Role

| Operation | SuperAdmin | TenantAdmin | User |
|-----------|-----------|-------------|------|
| View all tenants | ✅ Yes | ❌ No | ❌ No |
| View own tenant | ✅ Yes | ✅ Yes | ✅ Yes (limited) |
| View tenant users | ✅ All | ✅ Same tenant | ❌ Self only |
| Create user | ❌ No (via tenant) | ✅ Yes (user role) | ❌ No |
| Modify user | ✅ All | ✅ Same tenant | ✅ Self only |
| Delete user | ✅ All | ✅ Same tenant (not admin) | ❌ No |
| Create tenant | ✅ Yes | ❌ No | ❌ No |
| Manage engines | ✅ All | ✅ Same tenant | ❌ No |
| View engines | ✅ All | ✅ Same tenant | ✅ Same tenant |
| Create resources | ✅ Yes | ✅ Yes | ❌ No |
| Modify own resources | ✅ Yes | ✅ Yes | ✅ Yes |
| Delete resources | ✅ All | ✅ Same tenant | ✅ Own only |

---

## 3. Permission Models

### 3.1 Centralized Authorization Service

**Location:** `/Users/pampa/code/addp/common/service/auth_service.go`

The application uses a centralized `AuthService` that eliminates duplicate permission checks across 20+ service methods.

#### Core Authorization Functions

```go
type AuthService struct{}

// Role checking functions
func (s *AuthService) IsSuperAdmin(user models.UserInterface) bool
func (s *AuthService) IsTenantAdmin(user models.UserInterface) bool
func (s *AuthService) IsNormalUser(user models.UserInterface) bool

// Access control functions
func (s *AuthService) CheckTenantAccess(user models.UserInterface, resourceTenantID *uint) error
func (s *AuthService) CheckResourceOwnership(user models.UserInterface, creatorID *uint) error
func (s *AuthService) CheckCreatePermission(user models.UserInterface, resourceType string) error
func (s *AuthService) CheckUpdatePermission(user models.UserInterface, resourceCreatorID *uint, resourceTenantID *uint) error
func (s *AuthService) CheckDeletePermission(user models.UserInterface, resourceCreatorID *uint, resourceTenantID *uint, isBuiltin bool) error
```

### 3.2 Tenant Access Control

**Location:** `/Users/pampa/code/addp/common/service/auth_service.go:41-62`

```go
func (s *AuthService) CheckTenantAccess(user models.UserInterface, resourceTenantID *uint) error {
    if user == nil {
        return ErrUserNotFound
    }
    
    // SuperAdmin can access all tenant resources
    if s.IsSuperAdmin(user) {
        return nil
    }
    
    // Check tenant ID match
    userTenantID := user.GetTenantID()
    if userTenantID == nil || resourceTenantID == nil {
        return errors.New("租户信息不完整")
    }
    
    if *userTenantID != *resourceTenantID {
        return errors.New("没有权限访问该租户的资源")
    }
    
    return nil
}
```

**Authorization Logic:**
1. SuperAdmin bypasses tenant checks (can access all tenants)
2. Other users must have matching tenant_id with resource
3. NULL tenant checks prevent incomplete data access

### 3.3 Resource Ownership Control

**Location:** `/Users/pampa/code/addp/common/service/auth_service.go:66-82`

```go
func (s *AuthService) CheckResourceOwnership(user models.UserInterface, creatorID *uint) error {
    if user == nil {
        return ErrUserNotFound
    }
    
    // SuperAdmin and TenantAdmin can manage all resources
    if s.IsSuperAdmin(user) || s.IsTenantAdmin(user) {
        return nil
    }
    
    // Regular user can only manage their own resources
    if creatorID == nil || user.GetID() != *creatorID {
        return errors.New("只能管理自己创建的资源")
    }
    
    return nil
}
```

**Authorization Logic:**
1. SuperAdmin and TenantAdmin bypass ownership checks
2. Regular users must be the creator (user_id == created_by)
3. NULL creator_id check prevents orphaned resource access

### 3.4 Update Permission Logic

**Location:** `/Users/pampa/code/addp/common/service/auth_service.go:108-125`

```go
func (s *AuthService) CheckUpdatePermission(user models.UserInterface, resourceCreatorID *uint, resourceTenantID *uint) error {
    if user == nil {
        return ErrUserNotFound
    }
    
    // SuperAdmin can update all resources
    if s.IsSuperAdmin(user) {
        return nil
    }
    
    // TenantAdmin can update same-tenant resources
    if s.IsTenantAdmin(user) {
        return s.CheckTenantAccess(user, resourceTenantID)
    }
    
    // Regular user can only update their own resources
    return s.CheckResourceOwnership(user, resourceCreatorID)
}
```

### 3.5 Delete Permission Logic

**Location:** `/Users/pampa/code/addp/common/service/auth_service.go:128-150`

```go
func (s *AuthService) CheckDeletePermission(user models.UserInterface, resourceCreatorID *uint, resourceTenantID *uint, isBuiltin bool) error {
    if user == nil {
        return ErrUserNotFound
    }
    
    // Built-in resources cannot be deleted
    if isBuiltin {
        return errors.New("内置资源不可删除")
    }
    
    // SuperAdmin can delete all resources
    if s.IsSuperAdmin(user) {
        return nil
    }
    
    // TenantAdmin can delete same-tenant resources
    if s.IsTenantAdmin(user) {
        return s.CheckTenantAccess(user, resourceTenantID)
    }
    
    // Regular user can only delete their own resources
    return s.CheckResourceOwnership(user, resourceCreatorID)
}
```

**Special Protections:**
- Built-in resources (`is_builtin = true`) cannot be deleted by anyone
- SuperAdmin user "SuperAdmin" cannot be deleted (hardcoded check)

---

## 4. Authorization Decision Points

### 4.1 Authentication Middleware

#### System Service JWT Authentication

**Location:** `/Users/pampa/code/addp/system/backend/internal/middleware/auth.go:13-42`

```go
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        authHeader := c.GetHeader("Authorization")
        if authHeader == "" {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少认证令牌"})
            c.Abort()
            return
        }
        
        // Parse Bearer token
        parts := strings.SplitN(authHeader, " ", 2)
        if len(parts) != 2 || parts[0] != "Bearer" {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "认证令牌格式错误"})
            c.Abort()
            return
        }
        
        claims, err := utils.ParseToken(parts[1], cfg.JWTSecret)
        if err != nil {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的认证令牌"})
            c.Abort()
            return
        }
        
        // Inject user context
        c.Set("user_id", claims.UserID)
        c.Set("username", claims.Username)
        c.Set("tenant_id", claims.TenantID)
        c.Next()
    }
}
```

**Context Variables Set:**
- `user_id` (uint): User's database ID
- `username` (string): User's username
- `tenant_id` (uint): User's tenant ID (0 for SuperAdmin)

#### Shared Authentication Middleware (Other Services)

**Location:** `/Users/pampa/code/addp/common/middleware/auth/middleware.go:30-105`

```go
func SystemAuthMiddleware(systemURL string) gin.HandlerFunc {
    baseURL := strings.TrimSuffix(systemURL, "/")
    meEndpoint := baseURL + "/api/system/users/me"
    
    return func(c *gin.Context) {
        authHeader := c.GetHeader("Authorization")
        
        // Support token in query parameter
        if strings.TrimSpace(authHeader) == "" {
            if tokenParam := strings.TrimSpace(c.Query("token")); tokenParam != "" {
                authHeader = "Bearer " + tokenParam
                c.Request.Header.Set("Authorization", authHeader)
            }
        }
        
        // Validate token with System service
        req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, meEndpoint, nil)
        req.Header.Set("Authorization", authHeader)
        
        resp, err := httpClient.Do(req)
        if resp.StatusCode != http.StatusOK {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
            c.Abort()
            return
        }
        
        var userInfo UserInfo
        json.NewDecoder(resp.Body).Decode(&userInfo)
        
        // Inject user context
        c.Set(ContextUserIDKey, userInfo.ID)
        c.Set(ContextUsernameKey, userInfo.Username)
        c.Set(ContextTenantIDKey, *userInfo.TenantID or 0)
        c.Set(ContextUserInfoKey, userInfo)
        c.Next()
    }
}
```

**Delegation Pattern:** Other services delegate authentication to the System service via HTTP call to `/api/system/users/me`.

#### Cached Authentication Middleware (Performance Optimization)

**Location:** `/Users/pampa/code/addp/common/middleware/auth/cached_middleware.go:39-180`

```go
func CachedSystemAuthMiddleware(systemURL string, redisClient *redis.Client, cacheTTL time.Duration) gin.HandlerFunc {
    return func(c *gin.Context) {
        // 0. Check for internal API key
        if internalKey := c.GetHeader("X-Internal-API-Key"); internalKey != "" {
            // Internal service-to-service calls
            tenantID := parseHeaderUint(c.GetHeader("X-Tenant-ID"))
            c.Set(ContextUserIDKey, uint(1))
            c.Set(ContextUsernameKey, "internal-api-call")
            c.Set(ContextTenantIDKey, tenantID)
            c.Next()
            return
        }
        
        // 1. Extract token hash
        token := extractToken(authHeader)
        cacheKey := generateTokenCacheKey(token) // SHA256 hash
        
        // 2. Check local cache (< 1ms)
        // 3. Check Redis cache (< 5ms)
        // 4. Call System service (< 20ms)
        // 5. Cache result for future requests
        
        userInfo := validateWithCache(cacheKey)
        c.Set(ContextUserIDKey, userInfo.ID)
        c.Set(ContextUsernameKey, userInfo.Username)
        c.Set(ContextTenantIDKey, userInfo.TenantID)
        c.Next()
    }
}
```

**Caching Strategy:**
- Local in-memory cache (5-minute TTL)
- Redis cache (5-minute TTL)
- 90% reduction in System service calls

**Internal API Key Support:**
- Header: `X-Internal-API-Key`
- Optional tenant header: `X-Tenant-ID`
- Bypasses JWT authentication for service-to-service calls

#### Tenant Isolation Middleware

**Location:** `/Users/pampa/code/addp/common/middleware/auth/tenant_isolation.go:17-44`

```go
func TenantIsolationMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        tenantID, exists := c.Get(ContextTenantIDKey)
        if !exists {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant information missing"})
            c.Abort()
            return
        }
        
        // SuperAdmin (tenant_id = 0) is not restricted
        if tenantID.(uint) == 0 {
            c.Set(ContextTenantIsolationEnabledKey, false)
        } else {
            c.Set(ContextTenantIsolationEnabledKey, true)
        }
        
        c.Next()
    }
}
```

**Applied to:** Meta service (line 48 in `/Users/pampa/code/addp/meta/backend/internal/api/router.go`)

### 4.2 Gateway API Key Authentication

**Location:** `/Users/pampa/code/addp/gateway/internal/middleware/api_key_auth.go:42-79`

```go
func (m *APIKeyAuthMiddleware) Handler() gin.HandlerFunc {
    return func(c *gin.Context) {
        apiKey := c.GetHeader("X-API-Key")
        if apiKey == "" {
            // No API Key, skip (may use JWT instead)
            c.Next()
            return
        }
        
        keyHash := hashAPIKey(apiKey) // SHA256
        
        // Three-tier cache validation
        info, err := m.validateWithCache(keyHash)
        // 1. Local cache (< 1ms)
        // 2. Redis cache (< 5ms)  
        // 3. System API call (< 20ms)
        
        if !info.Valid {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API Key"})
            return
        }
        
        c.Set("api_key_info", info)
        c.Set("app_id", info.AppID)
        c.Set("app_name", info.AppName)
        c.Next()
    }
}
```

**Key Validation Endpoint:** `/api/internal/api-keys/validate` (System service)

**Three-Tier Caching:**
1. Local in-memory cache (5 minutes)
2. Redis cache (1 hour)
3. System service database lookup

### 4.3 Service-Level Authorization

All CRUD operations in service layers call `AuthService` methods before executing database operations.

**Example: User Service**

**Location:** `/Users/pampa/code/addp/system/backend/internal/service/user_service.go`

```go
type UserService struct {
    repo    repository.UserRepositoryInterface
    authSvc *commonservice.AuthService  // Injected authorization service
}

func (s *UserService) GetByID(id uint, currentUserID uint) (*models.User, error) {
    user, err := s.repo.GetByID(id)
    currentUser, err := s.repo.GetByID(currentUserID)
    
    // SuperAdmin can view all users
    if s.authSvc.IsSuperAdmin(currentUser) {
        return user, nil
    }
    
    // TenantAdmin can view same-tenant users
    if s.authSvc.IsTenantAdmin(currentUser) {
        if err := s.authSvc.CheckTenantAccess(currentUser, user.TenantID); err != nil {
            return nil, errors.New("没有权限查看该用户")
        }
        return user, nil
    }
    
    // Regular user can only view themselves
    if user.ID != currentUserID {
        return nil, errors.New("没有权限查看该用户")
    }
    
    return user, nil
}
```

**Example: Engine Service**

**Location:** `/Users/pampa/code/addp/system/backend/internal/service/engine_service.go:164-183`

```go
func (s *EngineService) GetByID(id uint, currentUserID uint) (*models.Engine, error) {
    currentUser, err := s.getCurrentUser(currentUserID)
    engine, err := s.repo.GetByID(id)
    
    if err := s.authorizeResourceAccess(engine, currentUser); err != nil {
        return nil, err
    }
    
    return s.sanitizeResource(engine), nil
}

func (s *EngineService) authorizeResourceAccess(engine *models.Engine, currentUser *models.User) error {
    // SuperAdmin can access all engines
    if currentUser.UserType == models.UserTypeSuperAdmin {
        return nil
    }
    
    // Check tenant match
    if engine.TenantID != nil && currentUser.TenantID != nil {
        if *engine.TenantID != *currentUser.TenantID {
            return ErrResourceForbidden
        }
    }
    
    return nil
}
```

---

## 5. Object Ownership Patterns

### 5.1 Database Schema Patterns

All resources follow a consistent ownership pattern:

```sql
-- Common ownership fields across all resource tables
tenant_id      INT REFERENCES system.tenants(id)  -- Tenant isolation
created_by     INT REFERENCES system.users(id)    -- Creator/owner
is_builtin     BOOL DEFAULT false                 -- Prevent deletion
```

### 5.2 Resource Models with Ownership Fields

#### Engine Model

**Location:** `/Users/pampa/code/addp/common/models/engine.go:78-102`

```go
type Engine struct {
    ID             uint           `gorm:"column:id" json:"id"`
    TenantID       *uint          `gorm:"column:tenant_id;index" json:"tenant_id"`
    Name           string         `json:"name"`
    EngineType     string         `json:"engine_type"`
    ConnectionInfo ConnectionInfo `json:"connection_info"`
    IsActive       bool           `json:"is_active"`
    CreatedBy      *uint          `gorm:"column:created_by" json:"created_by,omitempty"`
    IsBuiltin      bool           `gorm:"column:is_builtin;default:false;index" json:"is_builtin"`
    CreatedAt      time.Time      `json:"created_at"`
    UpdatedAt      time.Time      `json:"updated_at"`
}
```

#### Application Model

**Location:** `/Users/pampa/code/addp/system/backend/internal/models/application.go:10-25`

```go
type Application struct {
    ID                  uint       `gorm:"primaryKey" json:"id"`
    Name                string     `json:"name"`
    Description         string     `json:"description,omitempty"`
    TenantID            uint       `gorm:"not null;index" json:"tenant_id"`
    CreatedBy           *uint      `json:"created_by,omitempty"`
    CreatedAt           time.Time  `json:"created_at"`
    UpdatedAt           time.Time  `json:"updated_at"`
    DeletedAt           *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}
```

#### API Key Model

**Location:** `/Users/pampa/code/addp/system/backend/internal/models/application.go:33-49`

```go
type APIKey struct {
    ID            uint       `gorm:"primaryKey" json:"id"`
    ApplicationID uint       `gorm:"not null;index" json:"application_id"`
    KeyHash       string     `gorm:"uniqueIndex" json:"-"`
    Status        string     `json:"status"`
    CreatedBy     *uint      `json:"created_by,omitempty"`
    CreatedAt     time.Time  `json:"created_at"`
    RevokedAt     *time.Time `json:"revoked_at,omitempty"`
    RevokedBy     *uint      `json:"revoked_by,omitempty"`
}
```

#### DevItem Model (Develop Service)

**Location:** `/Users/pampa/code/addp/develop/backend/internal/models/dev_item.go:13-45`

```go
type DevItem struct {
    ID          uint           `gorm:"primaryKey" json:"id"`
    TenantID    uint           `gorm:"not null;index" json:"tenant_id"`
    Name        string         `json:"name"`
    DevType     string         `json:"dev_type"`
    CreatedBy   *uint          `json:"created_by,omitempty"`
    UpdatedBy   *uint          `json:"updated_by,omitempty"`
    CreatedAt   time.Time      `json:"created_at"`
    UpdatedAt   time.Time      `json:"updated_at"`
    DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}
```

#### Task Model (Transfer Service)

**Location:** `/Users/pampa/code/addp/transfer/backend/internal/models/task.go:79-95`

```go
type Task struct {
    ID          uint       `gorm:"primaryKey" json:"id"`
    Name        string     `json:"name"`
    Description string     `json:"description"`
    Config      JSONMap    `gorm:"type:jsonb" json:"config"`
    CreatedBy   *uint      `json:"created_by,omitempty"`
    TenantID    uint       `gorm:"not null;index" json:"tenant_id"`
    CreatedAt   time.Time  `json:"created_at"`
    UpdatedAt   time.Time  `json:"updated_at"`
}
```

### 5.3 Ownership Validation in Services

#### Pattern 1: TenantID-Based Filtering (Develop Service)

**Location:** `/Users/pampa/code/addp/develop/backend/internal/service/dev_item_service.go`

```go
func (s *DevItemService) GetDevItem(id uint, tenantID uint) (*models.DevItem, error) {
    // Repository filters by both ID AND tenant_id
    item, err := s.devItemRepo.FindByID(id, tenantID)
    if err != nil {
        return nil, fmt.Errorf("开发项不存在")
    }
    return item, nil
}

func (s *DevItemService) UpdateDevItem(id uint, req *models.UpdateDevItemRequest, tenantID uint, userID uint) (*models.DevItem, error) {
    // Tenant-scoped lookup prevents cross-tenant access
    item, err := s.devItemRepo.FindByID(id, tenantID)
    if err != nil {
        return nil, fmt.Errorf("开发项不存在")
    }
    // Update logic...
}

func (s *DevItemService) DeleteDevItem(id uint, tenantID uint) error {
    _, err := s.devItemRepo.FindByID(id, tenantID)
    if err != nil {
        return fmt.Errorf("开发项不存在")
    }
    return s.devItemRepo.Delete(id, tenantID)
}
```

**Authorization Approach:**
- Repository layer enforces tenant filtering
- All queries include `WHERE tenant_id = ?`
- Cross-tenant access blocked at database level

#### Pattern 2: AuthService-Based Checks (System Service)

**Location:** `/Users/pampa/code/addp/system/backend/internal/service/user_service.go`

```go
func (s *UserService) Update(id uint, req *models.UserUpdateRequest, currentUserID uint) (*models.User, error) {
    user, err := s.repo.GetByID(id)
    currentUser, err := s.repo.GetByID(currentUserID)
    
    // Validate update permission using AuthService
    if err := s.validateUpdatePermission(currentUser, user, req); err != nil {
        return nil, err
    }
    
    // Update logic...
}

func (s *UserService) validateUpdatePermission(currentUser, targetUser *models.User, req *models.UserUpdateRequest) error {
    // SuperAdmin can modify all users
    if s.authSvc.IsSuperAdmin(currentUser) {
        return nil
    }
    
    // TenantAdmin can modify same-tenant users
    if s.authSvc.IsTenantAdmin(currentUser) {
        if err := s.authSvc.CheckTenantAccess(currentUser, targetUser.TenantID); err != nil {
            return errors.New("只能修改同租户的用户")
        }
        // Cannot modify other tenant admins
        if targetUser.UserType == models.UserTypeTenantAdmin && targetUser.ID != currentUser.ID {
            return errors.New("不能修改其他租户管理员")
        }
        return nil
    }
    
    // Regular user can only modify themselves
    if currentUser.ID != targetUser.ID {
        return errors.New("只能修改自己的信息")
    }
    
    // Regular users cannot modify activation status or role
    if req.IsActive != nil || req.UserType != nil {
        return errors.New("没有权限修改用户状态和类型")
    }
    
    return nil
}
```

---

## 6. Endpoints with Object IDs

### 6.1 System Service Endpoints

**Base URL:** `/api/system`  
**Authentication:** JWT (AuthMiddleware)  
**Router:** `/Users/pampa/code/addp/system/backend/internal/api/router.go:107-184`

| Endpoint | Method | ID Parameter | Ownership Validation | Handler |
|----------|--------|--------------|---------------------|---------|
| `/users/:id` | GET | user_id | Role-based (self/tenant/all) | UserHandler.GetByID |
| `/users/:id` | PUT | user_id | Role + ownership check | UserHandler.Update |
| `/users/:id` | DELETE | user_id | Role + ownership check | UserHandler.Delete |
| `/users/:id/change-password` | PUT | user_id | Self-only | UserHandler.ChangePassword |
| `/logs/:id` | GET | log_id | Tenant-scoped | LogHandler.GetByID |
| `/engines/:id` | GET | engine_id | Tenant-scoped | EngineHandler.GetByID |
| `/engines/:id` | PUT | engine_id | Tenant + AuthService | EngineHandler.Update |
| `/engines/:id` | DELETE | engine_id | Tenant + AuthService | EngineHandler.Delete |
| `/engines/:id/test` | POST | engine_id | Tenant-scoped | EngineHandler.TestConnection |
| `/engines/:id/schemas` | GET | engine_id | Tenant-scoped | EngineHandler.ListSchemas |
| `/engines/:id/tables` | GET | engine_id | Tenant-scoped | EngineHandler.ListTables |
| `/tenants/:id` | GET | tenant_id | SuperAdmin only | TenantHandler.GetByID |
| `/tenants/:id` | PUT | tenant_id | SuperAdmin only | TenantHandler.Update |
| `/tenants/:id` | DELETE | tenant_id | SuperAdmin only | TenantHandler.Delete |
| `/applications/:id` | GET | app_id | Tenant-scoped | ApplicationHandler.GetApplication |
| `/applications/:id` | PUT | app_id | Tenant-scoped | ApplicationHandler.UpdateApplication |
| `/applications/:id` | DELETE | app_id | Tenant-scoped | ApplicationHandler.DeleteApplication |
| `/applications/:id/keys` | POST | app_id | Tenant-scoped | ApplicationHandler.GenerateAPIKey |
| `/applications/:id/keys` | GET | app_id | Tenant-scoped | ApplicationHandler.ListAPIKeys |
| `/applications/:id/keys/:key_id` | DELETE | app_id, key_id | Tenant-scoped | ApplicationHandler.RevokeAPIKey |

**Authorization Pattern:**
- All endpoints require JWT authentication
- User context extracted from JWT: `user_id`, `tenant_id`
- Services call `AuthService` methods for permission checks
- Tenant filtering applied in repository layer

### 6.2 Develop Service Endpoints

**Base URL:** `/api/develop`  
**Authentication:** SystemAuthMiddleware (delegates to System service)  
**Router:** `/Users/pampa/code/addp/develop/backend/internal/api/router.go:102-210`

| Endpoint | Method | ID Parameter | Ownership Validation | Handler |
|----------|--------|--------------|---------------------|---------|
| `/items/:id` | GET | item_id | Tenant-scoped | DevItemHandler.GetDevItem |
| `/items/:id` | PUT | item_id | Tenant-scoped | DevItemHandler.UpdateDevItem |
| `/items/:id` | DELETE | item_id | Tenant-scoped | DevItemHandler.DeleteDevItem |
| `/items/:id/execute` | POST | item_id | Tenant-scoped | DevExecutionHandler.ExecuteDevItem |
| `/items/:id/execute-with-params` | POST | item_id | Tenant-scoped | DevExecutionHandler.ExecuteWithParams |
| `/executions/:id` | GET | execution_id | Tenant-scoped | DevExecutionHandler.GetExecution |
| `/executions/:id/logs` | GET | execution_id | Tenant-scoped | DevExecutionHandler.GetExecutionLogs |
| `/executions/:id/cancel` | POST | execution_id | Tenant-scoped | DevExecutionHandler.CancelExecution |
| `/executions/:id/retry` | POST | execution_id | Tenant-scoped | DevExecutionHandler.RetryExecution |
| `/engines/:id/schemas` | GET | engine_id | Tenant-scoped | EngineHandler.ListSchemas |
| `/engines/:id/tables` | GET | engine_id | Tenant-scoped | EngineHandler.ListTables |
| `/query/tasks/:id` | GET | task_id | Tenant-scoped | QueryHandler.GetQueryTask |
| `/query/tasks/:id` | PUT | task_id | Tenant-scoped | QueryHandler.UpdateQueryTask |
| `/query/tasks/:id` | DELETE | task_id | Tenant-scoped | QueryHandler.DeleteQueryTask |
| `/notebooks/:id/download` | GET | notebook_id | Tenant-scoped | NotebookHandler.DownloadNotebook |
| `/notebooks/:id` | DELETE | notebook_id | Tenant-scoped | NotebookHandler.DeleteNotebook |
| `/jupyter/venv/:tenant_id/init` | POST | tenant_id | Admin-only | JupyterVenvHandler.InitVenvByID |

**Authorization Pattern:**
- SystemAuthMiddleware calls `/api/system/users/me` to validate token
- User context from System service: `user_id`, `tenant_id`
- All service methods accept `tenantID` parameter
- Repository filters by `tenant_id` in WHERE clause

### 6.3 Manager Service Endpoints

**Base URL:** `/api/manager`  
**Authentication:** CachedSystemAuthMiddleware  
**Router:** `/Users/pampa/code/addp/manager/backend/internal/api/router.go:54-163`

| Endpoint | Method | ID Parameter | Ownership Validation | Handler |
|----------|--------|--------------|---------------------|---------|
| `/embedding/tasks/:task_id` | GET | task_id | Tenant-scoped | EmbeddingHandler.GetEmbeddingTaskStatus |
| `/tree/:engine_id` | GET | engine_id | Tenant-scoped | ExplorerHandler.GetTree |
| `/tree/:engine_id/node` | GET | engine_id | Tenant-scoped | ExplorerHandler.GetNodeChildren |
| `/tree/:engine_id/search` | GET | engine_id | Tenant-scoped | ExplorerHandler.SearchNodes |
| `/tree/:engine_id/refresh` | POST | engine_id | Tenant-scoped | ExplorerHandler.RefreshNode |
| `/engines/:id/spatial/features/:feature_id/centroid` | GET | engine_id, feature_id | Tenant-scoped | FeatureHandler.GetFeatureCentroid |
| `/engines/:id/spatial/features/:feature_id/geometry` | GET | engine_id, feature_id | Tenant-scoped | FeatureHandler.GetFeatureGeometry |
| `/search/history/:id` | DELETE | history_id | User-scoped | SearchHandler.DeleteHistoryItem |
| `/engines/:id/spatial/:schema/:table/*` | GET/POST/DELETE | engine_id | Tenant-scoped | Various spatial handlers |

**Authorization Pattern:**
- Cached middleware (Redis + local cache)
- Tenant filtering in all database queries
- Spatial data access controlled by engine ownership

### 6.4 Meta Service Endpoints

**Base URL:** `/api/meta`  
**Authentication:** CachedSystemAuthMiddleware + TenantIsolationMiddleware  
**Router:** `/Users/pampa/code/addp/meta/backend/internal/api/router.go:36-88`

| Endpoint | Method | ID Parameter | Ownership Validation | Handler |
|----------|--------|--------------|---------------------|---------|
| `/engines/:engine_id/schemas` | GET | engine_id | Tenant-scoped | Handler.GetSchemas |
| `/engines/:engine_id/schemas/available` | GET | engine_id | Tenant-scoped | Handler.ListAvailableSchemas |
| `/engines/:engine_id/storage/nodes` | GET | engine_id | Tenant-scoped | Handler.ListObjectStorageNodes |
| `/engines/:engine_id/tree` | GET | engine_id | Tenant-scoped | Handler.GetMetadataTree |
| `/nodes/:node_id` | GET | node_id | Tenant-scoped | Handler.GetMetaNodeByID |
| `/nodes/:node_id/children` | GET | node_id | Tenant-scoped | Handler.GetNodeChildren |
| `/nodes/:node_id/items` | GET | node_id | Tenant-scoped | Handler.GetNodeItems |
| `/scan/runs/:run_id` | GET | run_id | Tenant-scoped | Handler.GetScanRun |
| `/scan/tasks/:task_id` | PUT | task_id | Tenant-scoped | Handler.UpdateScanTask |
| `/scan/tasks/:task_id` | DELETE | task_id | Tenant-scoped | Handler.DeleteScanTask |
| `/scan/tasks/:task_id/trigger` | POST | task_id | Tenant-scoped | Handler.TriggerScanTask |
| `/cache/engines/:engine_id` | DELETE | engine_id | Tenant-scoped | Handler.ClearResourceCache |

**Authorization Pattern:**
- TenantIsolationMiddleware explicitly enforces tenant boundaries
- SuperAdmin can query across tenants with `?tenant_id=N` parameter
- Regular users restricted to own tenant

### 6.5 Transfer Service Endpoints

**Base URL:** `/api/transfer`  
**Authentication:** CachedSystemAuthMiddleware  
**Router:** `/Users/pampa/code/addp/transfer/backend/internal/api/router.go:61-186`

| Endpoint | Method | ID Parameter | Ownership Validation | Handler |
|----------|--------|--------------|---------------------|---------|
| `/tasks/:id` | GET | task_id | Tenant-scoped | TaskHandler.GetTask |
| `/tasks/:id` | PUT | task_id | Tenant-scoped | TaskHandler.UpdateTask |
| `/tasks/:id` | DELETE | task_id | Tenant-scoped | TaskHandler.DeleteTask |
| `/tasks/:id/start` | POST | task_id | Tenant-scoped | TaskHandler.StartTask |
| `/tasks/:id/stop` | POST | task_id | Tenant-scoped | TaskHandler.StopTask |
| `/tasks/:id/pause` | POST | task_id | Tenant-scoped | TaskHandler.PauseTask |
| `/tasks/:id/resume` | POST | task_id | Tenant-scoped | TaskHandler.ResumeTask |
| `/tasks/:id/executions` | GET | task_id | Tenant-scoped | ExecutionHandler.GetTaskExecutions |
| `/tasks/:id/mappings` | POST | task_id | Tenant-scoped | TaskHandler.CreateFieldMapping |
| `/tasks/:id/mappings` | GET | task_id | Tenant-scoped | TaskHandler.GetTaskMappings |
| `/mappings/:id` | DELETE | mapping_id | Tenant-scoped | TaskHandler.DeleteFieldMapping |
| `/local-engines/:id` | PUT | engine_id | Tenant-scoped | LocalEngineHandler.Update |
| `/local-engines/:id` | DELETE | engine_id | Tenant-scoped | LocalEngineHandler.Delete |
| `/local-engines/:id/test` | POST | engine_id | Tenant-scoped | LocalEngineHandler.TestExisting |
| `/local-engines/:id/sync` | POST | engine_id | Tenant-scoped | LocalEngineHandler.Sync |
| `/local-engines/:id/tables` | GET | engine_id | Tenant-scoped | LocalEngineHandler.ListTables |
| `/local-engines/:id/fields` | GET | engine_id | Tenant-scoped | LocalEngineHandler.ListFields |
| `/executions/:id` | GET | execution_id | Tenant-scoped | ExecutionHandler.GetExecution |
| `/executions/:id/cancel` | POST | execution_id | Tenant-scoped | ExecutionHandler.CancelExecution |
| `/executions/:id/retry` | POST | execution_id | Tenant-scoped | ExecutionHandler.RetryExecution |
| `/executions/:id/progress` | GET | execution_id | Tenant-scoped | ExecutionHandler.GetExecutionProgress |
| `/executions/:id/logs` | GET | execution_id | Tenant-scoped | ExecutionHandler.GetExecutionLogs |

**Authorization Pattern:**
- All resources filtered by `tenant_id` from JWT
- No cross-tenant access possible
- Execution logs accessible by same tenant

### 6.6 Orchestrator Service Endpoints

**Base URL:** `/api/orchestrator`  
**Authentication:** CachedSystemAuthMiddleware  
**Router:** `/Users/pampa/code/addp/orchestrator/backend/internal/api/router.go:50-83`

| Endpoint | Method | ID Parameter | Ownership Validation | Handler |
|----------|--------|--------------|---------------------|---------|
| `/orchestrations/:id` | GET | orchestration_id | Tenant-scoped | OrchestrationHandler.Get |
| `/orchestrations/:id` | PUT | orchestration_id | Tenant-scoped | OrchestrationHandler.Update |
| `/orchestrations/:id` | DELETE | orchestration_id | Tenant-scoped | OrchestrationHandler.Delete |
| `/orchestrations/:id/execute` | POST | orchestration_id | Tenant-scoped | OrchestrationHandler.Execute |
| `/orchestrations/:id/executions` | GET | orchestration_id | Tenant-scoped | OrchestrationHandler.ListExecutions |
| `/orch-executions/:id` | GET | execution_id | Tenant-scoped | OrchestrationHandler.GetExecution |

**Authorization Pattern:**
- Tenant filtering on orchestration definitions
- Execution history scoped to tenant

### 6.7 Service Module Endpoints

**Base URL:** `/api/service`  
**Authentication:** SystemAuthMiddleware  
**Router:** `/Users/pampa/code/addp/service/backend/internal/api/router.go:63-134`

| Endpoint | Method | ID Parameter | Ownership Validation | Handler |
|----------|--------|--------------|---------------------|---------|
| `/query/:id` | GET | service_id | Tenant-scoped | QueryServiceHandler.GetService |
| `/query/:id` | PUT | service_id | Tenant-scoped | QueryServiceHandler.UpdateService |
| `/query/:id` | DELETE | service_id | Tenant-scoped | QueryServiceHandler.DeleteService |
| `/registered/:id` | GET | service_id | Tenant-scoped | RegisteredServiceHandler.GetService |
| `/registered/:id` | PUT | service_id | Tenant-scoped | RegisteredServiceHandler.UpdateService |
| `/registered/:id` | DELETE | service_id | Tenant-scoped | RegisteredServiceHandler.DeleteService |
| `/registered/:id/refresh` | POST | service_id | Tenant-scoped | RegisteredServiceHandler.RefreshMetadata |
| `/registered/:id/health` | POST | service_id | Tenant-scoped | RegisteredServiceHandler.HealthCheck |
| `/tile/:id` | GET | tile_service_id | Tenant-scoped | TileServiceHandler.GetTileService |
| `/tile/:id` | PUT | tile_service_id | Tenant-scoped | TileServiceHandler.UpdateTileService |
| `/tile/:id` | DELETE | tile_service_id | Tenant-scoped | TileServiceHandler.DeleteTileService |
| `/tile-layers/:serviceId/:layerId` | GET | service_id, layer_id | Tenant-scoped | TileServiceHandler.GetLayer |
| `/tile-layers/:serviceId/:layerId` | PUT | service_id, layer_id | Tenant-scoped | TileServiceHandler.UpdateLayer |
| `/tile-layers/:serviceId/:layerId` | DELETE | service_id, layer_id | Tenant-scoped | TileServiceHandler.DeleteLayer |
| `/engines/:engine_id/tree` | GET | engine_id | Tenant-scoped | DataSourceHandler.GetEngineTree |
| `/nodes/:node_id/children` | GET | node_id | Tenant-scoped | DataSourceHandler.GetNodeChildren |

**Public Endpoints (No Authentication):**
- `/api/query/:serviceName` - Public data query (auth checked in handler)
- `/ogc/features/:serviceName/*` - OGC API Features (auth checked in handler)
- `/tiles/:serviceName/:layerName/:z/:x/*yformat` - XYZ tiles (auth checked in handler)
- `/wmts/:serviceName` - WMTS GetCapabilities (auth checked in handler)
- `/ogc/tiles/:serviceName/*` - OGC Tiles API (auth checked in handler)

**Authorization Pattern:**
- Public endpoints support both API Key and JWT
- Handler checks service visibility (public vs tenant-restricted)
- Tenant-scoped services require authentication

---

## 7. Key Security Findings

### 7.1 Strengths

1. **Centralized Authorization Service**
   - Single source of truth for permission logic
   - Eliminates duplicate checks across 20+ service methods
   - Consistent behavior across all services

2. **Multi-Layered Authentication**
   - JWT tokens for user authentication
   - API Keys for external applications
   - Internal API keys for service-to-service calls
   - Cached authentication reduces load by 90%

3. **Tenant Isolation**
   - All resources tagged with `tenant_id`
   - Database-level filtering prevents cross-tenant leaks
   - SuperAdmin can override for platform management

4. **Ownership Tracking**
   - `created_by` field tracks resource creators
   - Regular users restricted to owned resources
   - Admins can manage tenant-wide or platform-wide

5. **Built-in Resource Protection**
   - `is_builtin` flag prevents critical resource deletion
   - Hardcoded protection for "SuperAdmin" user

### 7.2 Potential Vulnerabilities

#### 7.2.1 Horizontal Privilege Escalation Opportunities

**Risk: Inconsistent Tenant Filtering**

Some services may not consistently apply tenant filtering in all code paths.

**Example:** If a handler directly calls a repository method without passing `tenant_id`, cross-tenant access may occur.

```go
// VULNERABLE: No tenant filtering
func (h *Handler) GetResource(c *gin.Context) {
    id := c.Param("id")
    resource, err := h.service.GetByID(id) // Missing tenant check
    c.JSON(200, resource)
}

// SECURE: Tenant-filtered
func (h *Handler) GetResource(c *gin.Context) {
    id := c.Param("id")
    tenantID := c.Get("tenant_id").(uint)
    resource, err := h.service.GetByID(id, tenantID) // Tenant-scoped lookup
    c.JSON(200, resource)
}
```

**Testing:** Try accessing resources with IDs from different tenants.

**Attack Vectors:**
1. Enumerate resource IDs across tenants
2. Access `/api/develop/items/123` with tenant A token
3. Check if resource belongs to tenant B

#### 7.2.2 Vertical Privilege Escalation Opportunities

**Risk: Insufficient Role Checks**

Some endpoints may not verify role requirements before allowing actions.

**Example:** User creation endpoint allows TenantAdmin to create users, but doesn't prevent creating another TenantAdmin.

**Location:** `/Users/pampa/code/addp/system/backend/internal/service/user_service.go:74-90`

```go
func (s *UserService) validateCreatePermission(creator *models.User, targetUserType models.UserType) error {
    // SuperAdmin cannot create users directly
    if creator.UserType == models.UserTypeSuperAdmin {
        return errors.New("超级管理员不能直接创建用户，请通过创建租户来添加用户")
    }
    
    // TenantAdmin can only create regular users
    if creator.UserType == models.UserTypeTenantAdmin {
        if targetUserType != models.UserTypeUser && targetUserType != "" {
            return errors.New("租户管理员只能创建普通用户")
        }
        return nil
    }
    
    // Regular users cannot create users
    return errors.New("没有权限创建用户")
}
```

**Potential Issue:** What if request doesn't specify `user_type`? Default is "user" but verify all code paths.

**Testing:**
1. Authenticate as TenantAdmin
2. POST `/api/system/users` with `{"username":"test","password":"pass","user_type":"tenant_admin"}`
3. Check if validation blocks this

#### 7.2.3 API Key Scope Issues

**Risk: Broad API Key Permissions**

API Keys have application-level permissions but may not enforce fine-grained resource access.

**Location:** `/Users/pampa/code/addp/system/backend/internal/models/application.go:14-15`

```go
type Application struct {
    TenantID            uint           `gorm:"not null;index" json:"tenant_id"`
    AllowedServices     pq.StringArray `gorm:"type:text[]" json:"allowed_services,omitempty"`
}
```

**Observation:**
- API Keys are scoped to applications
- Applications are scoped to tenants
- No per-resource permissions

**Testing:**
1. Create API Key for application A
2. Use key to access tenant resources
3. Check if key can access all tenant resources or just specific ones

#### 7.2.4 SuperAdmin Bypass Logic

**Risk: Inconsistent SuperAdmin Checks**

SuperAdmin bypass logic is implemented in multiple places, may have gaps.

**Observation:**
- Some methods check `UserTypeSuperAdmin`
- Some check `tenant_id == 0`
- Some use `IsSuperAdmin()` from AuthService

**Testing:**
1. Create user with `tenant_id = NULL` and `user_type = "user"`
2. Check if they gain SuperAdmin privileges

#### 7.2.5 Missing CreatedBy Checks

**Risk: Resources Without Ownership**

Some resources may not have `created_by` populated, leading to ambiguous ownership.

**Observation:**
- `created_by` is `*uint` (nullable pointer)
- NULL values bypass ownership checks
- Could allow any admin to manage orphaned resources

**Code Pattern:**
```go
// What happens if created_by is NULL?
func (s *AuthService) CheckResourceOwnership(user models.UserInterface, creatorID *uint) error {
    if creatorID == nil || user.GetID() != *creatorID {
        return errors.New("只能管理自己创建的资源")
    }
    return nil
}
```

**Testing:**
1. Create resource via internal API (may skip `created_by`)
2. Try to modify/delete as regular user
3. Check if orphaned resource is accessible

#### 7.2.6 Public Endpoint Authorization

**Risk: Handler-Level Auth Instead of Middleware**

Public endpoints delegate authentication to handlers, increasing risk of forgotten checks.

**Example:** `/api/query/:serviceName` (Service module)

**Location:** `/Users/pampa/code/addp/service/backend/internal/api/router.go:35`

```go
// Public endpoint - authentication handled inside handler
router.GET("/api/query/:serviceName", queryServiceHandler.QueryData)
```

**Risk:** Handler may forget to check service visibility or user permissions.

**Testing:**
1. Access public query endpoint without authentication
2. Check if private services are exposed
3. Verify service-level visibility enforcement

#### 7.2.7 Internal API Key Validation

**Risk: Weak Internal Key Validation**

Internal API keys bypass authentication but validation may be weak.

**Location:** `/Users/pampa/code/addp/common/middleware/auth/cached_middleware.go:47-73`

```go
if internalKey := c.GetHeader("X-Internal-API-Key"); internalKey != "" {
    // TODO: Validate internal key against environment variable
    // For now, trust any internal key (in production, add validation)
    
    tenantID := parseHeaderUint(c.GetHeader("X-Tenant-ID"))
    c.Set(ContextUserIDKey, uint(1))
    c.Set(ContextUsernameKey, "internal-api-call")
    c.Set(ContextTenantIDKey, tenantID)
    c.Next()
    return
}
```

**Issue:** Comment indicates weak validation - "trust any internal key"

**Testing:**
1. Send request with `X-Internal-API-Key: test`
2. Check if arbitrary internal calls succeed
3. Try to manipulate tenant_id via header

#### 7.2.8 Password Change Authorization

**Risk: Weak Self-Only Enforcement**

Password change enforces "self-only" at service layer, but handler may pass wrong ID.

**Location:** `/Users/pampa/code/addp/system/backend/internal/service/user_service.go:326-350`

```go
func (s *UserService) ChangePassword(userID uint, req *models.ChangePasswordRequest, currentUserID uint) error {
    // Only allow changing own password
    if userID != currentUserID {
        return errors.New("只能修改自己的密码")
    }
    // ...
}
```

**Testing:**
1. Authenticate as user A (ID=5)
2. PUT `/api/system/users/10/change-password`
3. Verify if service rejects mismatched IDs

---

## 8. Recommendations

### 8.1 High Priority

1. **Validate Internal API Key:**
   - Remove "trust any internal key" logic
   - Implement proper secret validation against environment variable
   - Add audit logging for internal API calls

2. **Enforce Consistent Tenant Filtering:**
   - Add database-level row security policies
   - Require `tenant_id` parameter in all repository methods
   - Add integration tests for cross-tenant access attempts

3. **Add CreatedBy Validation:**
   - Make `created_by` non-nullable for all resources
   - Populate in all create operations (including internal API)
   - Add database constraint to prevent NULL values

4. **Standardize Authorization Checks:**
   - Use middleware instead of handler-level checks for public endpoints
   - Consolidate all role checks through `AuthService`
   - Add authorization integration tests

### 8.2 Medium Priority

5. **Implement Fine-Grained API Key Permissions:**
   - Add resource-level scoping to API Keys
   - Allow keys to be restricted to specific engine IDs or object types
   - Implement rate limiting per resource, not just per key

6. **Add Audit Logging:**
   - Log all authorization failures
   - Track cross-tenant access attempts
   - Alert on suspicious privilege escalation patterns

7. **Strengthen SuperAdmin Isolation:**
   - Separate SuperAdmin operations to dedicated endpoints
   - Require additional confirmation for destructive actions
   - Implement time-based 2FA for SuperAdmin logins

### 8.3 Low Priority

8. **Add Resource Ownership Transfer:**
   - Allow TenantAdmin to reassign resource ownership
   - Track ownership history for audit trail

9. **Implement Fine-Grained Permissions:**
   - Move from RBAC to attribute-based access control (ABAC)
   - Support custom permission policies per tenant
   - Allow delegated permissions (user A grants access to user B)

---

## 9. Testing Checklist

### 9.1 Horizontal Privilege Escalation Tests

- [ ] Access tenant A resources with tenant B credentials
- [ ] Enumerate resource IDs across tenant boundaries
- [ ] Test all endpoints with cross-tenant resource IDs
- [ ] Verify LIST operations don't leak cross-tenant data
- [ ] Check search/filter endpoints for tenant leakage

### 9.2 Vertical Privilege Escalation Tests

- [ ] Regular user attempts admin-only operations
- [ ] TenantAdmin attempts SuperAdmin operations
- [ ] TenantAdmin creates another TenantAdmin
- [ ] Regular user modifies role in profile update
- [ ] User with `tenant_id=NULL` but `user_type=user` privileges

### 9.3 Object Ownership Tests

- [ ] User A modifies User B's resources
- [ ] User accesses orphaned resources (`created_by=NULL`)
- [ ] Admin modifies built-in resources
- [ ] Delete SuperAdmin user account
- [ ] TenantAdmin modifies another TenantAdmin's resources

### 9.4 API Key Security Tests

- [ ] Use expired API Key
- [ ] Use revoked API Key
- [ ] Use API Key for different tenant's resources
- [ ] API Key with broad service access vs restricted
- [ ] Replay attacks with cached API Key validation

### 9.5 Internal API Tests

- [ ] Call internal endpoints with arbitrary `X-Internal-API-Key`
- [ ] Manipulate `X-Tenant-ID` header value
- [ ] Internal API without tenant header (should fail)
- [ ] Internal API access to protected endpoints

### 9.6 Public Endpoint Tests

- [ ] Access public query endpoints without auth
- [ ] Public endpoints expose private services
- [ ] WMTS/OGC endpoints leak tenant data
- [ ] Tile endpoints require authentication for private layers

---

## 10. Conclusion

The ADDP application implements a comprehensive role-based access control system with tenant isolation. The authorization architecture is well-structured with:

- Clear role hierarchy (SuperAdmin > TenantAdmin > User)
- Centralized authorization service eliminating duplicate checks
- Multi-layered authentication (JWT, API Keys, internal keys)
- Consistent tenant isolation patterns

However, several areas require attention:

1. **Internal API key validation** is weak and needs immediate hardening
2. **Tenant filtering** must be enforced consistently across all code paths
3. **Ownership tracking** should be mandatory (non-NULL `created_by`)
4. **Public endpoint authorization** should use middleware instead of handler checks

By addressing these recommendations and conducting thorough authorization testing, the application can achieve a robust security posture suitable for multi-tenant production environments.

---

**End of Report**
