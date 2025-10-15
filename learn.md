# JWT_SECRET 安全规范检查报告

## 检查日期
2025-10-12

## 一、检查结果总结

### ✅ 符合规范的地方

1. **环境变量隔离** ✅
   - JWT_SECRET 通过环境变量配置，未硬编码在代码中
   - 位置：`.env` 文件 (line 5)
   - 代码读取：`system/backend/internal/config/config.go:44`

2. **Git 忽略** ✅
   - `.env` 已加入 `.gitignore` (root `.gitignore` line 11: `**/.env`)
   - 验证：`git status` 显示 `.env` 未被追踪
   - 防止敏感信息泄露到版本控制系统

3. **示例文件提供** ✅
   - 提供 `.env.example` 作为配置模板
   - 包含占位符提示：`your-super-secret-jwt-key-change-this-in-production`
   - 开发者可复制并修改

4. **加密算法** ✅
   - 使用 HS256 (HMAC-SHA256) 签名算法
   - 位置：`system/backend/pkg/utils/jwt.go:25`
   - 符合 JWT 标准，广泛应用于生产环境

5. **Token 过期时间** ✅
   - 设置 30 分钟过期时间 (`TokenExpireMinutes: 30`)
   - 位置：`system/backend/internal/config/config.go:46`
   - 合理的安全窗口期

6. **Claims 结构** ✅
   - 包含标准字段：`ExpiresAt`, `IssuedAt`
   - 位置：`system/backend/pkg/utils/jwt.go:9-13`
   - 符合 RFC 7519 标准

### ⚠️ 需要改进的地方

1. **密钥强度不足** ⚠️ **CRITICAL**
   - **问题**：默认值 `your-super-secret-jwt-key-change-this-in-production` 过于简单
   - **位置**：`system/backend/internal/config/config.go:44`
   - **影响**：开发环境使用弱密钥，可能被暴力破解
   - **建议**：
     ```go
     // 不应该提供默认值，强制用户设置
     JWTSecret: getEnv("JWT_SECRET", ""),

     // 启动时检查
     if cfg.JWTSecret == "" {
         log.Fatal("FATAL: JWT_SECRET must be set! Generate with: openssl rand -base64 64")
     }
     ```

2. **密钥长度未验证** ⚠️ **HIGH**
   - **问题**：未检查 JWT_SECRET 长度是否足够（建议至少 32 字节/256 bits）
   - **风险**：短密钥容易被暴力破解
   - **建议**：
     ```go
     if len(cfg.JWTSecret) < 32 {
         log.Fatal("FATAL: JWT_SECRET must be at least 32 characters (256 bits)")
     }
     ```

3. **缺少密钥轮换机制** ⚠️ **MEDIUM**
   - **问题**：无法平滑更换 JWT_SECRET
   - **影响**：密钥泄露时，必须强制所有用户重新登录
   - **建议**：支持多密钥验证（旧密钥用于验证，新密钥用于签名）
     ```go
     type Config struct {
         JWTSecret        string   // 当前签名密钥
         JWTSecretOld     string   // 旧密钥（用于验证现有 token）
     }
     ```

4. **缺少环境区分警告** ⚠️ **MEDIUM**
   - **问题**：生产环境可能误用开发密钥
   - **建议**：
     ```go
     if cfg.Env == "production" && cfg.JWTSecret == "your-super-secret-jwt-key-change-this-in-production" {
         log.Fatal("FATAL: Default JWT_SECRET detected in production! This is a security risk!")
     }
     ```

5. **缺少密钥生成指导** ⚠️ **LOW**
   - **问题**：开发者不知道如何生成安全密钥
   - **建议**：在 `.env.example` 中添加注释
     ```bash
     # 生产环境必须修改！使用以下命令生成：
     # openssl rand -base64 64
     # 或
     # node -e "console.log(require('crypto').randomBytes(64).toString('base64'))"
     JWT_SECRET=your-super-secret-jwt-key-change-this-in-production
     ```

6. **未使用 Token 黑名单** ⚠️ **MEDIUM**
   - **问题**：无法主动撤销已颁发的 Token
   - **场景**：用户主动登出、密码修改、账号封禁
   - **建议**：使用 Redis 存储已撤销的 Token ID（jti claim）
     ```go
     // 登出时加入黑名单
     redis.Set(ctx, "token:blacklist:" + tokenID, "1", 30*time.Minute)

     // 验证时检查
     if redis.Exists(ctx, "token:blacklist:" + claims.ID).Val() > 0 {
         return ErrTokenRevoked
     }
     ```

7. **未验证签名算法** ⚠️ **HIGH**
   - **问题**：未在 ParseToken 中验证签名算法，可能遭受算法降级攻击
   - **位置**：`system/backend/pkg/utils/jwt.go:30`
   - **风险**：攻击者可将算法改为 `none`，绕过签名验证
   - **建议**：
     ```go
     token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
         // 验证签名算法
         if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
             return nil, fmt.Errorf("unexpected signing method: %v", token.Method.Alg())
         }
         return []byte(secret), nil
     })
     ```

8. **缺少 Token 刷新机制** ⚠️ **LOW**
   - **问题**：30 分钟后必须重新登录，用户体验差
   - **建议**：实现 Refresh Token 机制
     - Access Token: 短期（15 分钟）
     - Refresh Token: 长期（7 天），存储在 Redis

## 二、安全风险评级

| 风险项 | 严重程度 | 当前状态 | 建议优先级 |
|--------|---------|---------|-----------|
| 弱默认密钥 | CRITICAL | ⚠️ | P0 (立即修复) |
| 未验证密钥长度 | HIGH | ⚠️ | P0 (立即修复) |
| 未验证签名算法 | HIGH | ⚠️ | P0 (立即修复) |
| 无密钥轮换机制 | MEDIUM | ⚠️ | P1 (近期修复) |
| 无 Token 黑名单 | MEDIUM | ⚠️ | P1 (近期修复) |
| 无环境区分警告 | MEDIUM | ⚠️ | P2 (计划修复) |
| 无 Refresh Token | LOW | ⚠️ | P3 (优化) |
| 缺少生成指导 | LOW | ⚠️ | P3 (优化) |

## 三、IT 行业标准对比

### OWASP 建议
- ✅ 使用环境变量
- ✅ Token 过期时间 < 1 小时
- ⚠️ 密钥强度 >= 256 bits (当前未强制)
- ⚠️ 算法白名单验证 (当前缺失)
- ⚠️ Token 撤销机制 (当前缺失)

### CWE-798 (硬编码凭证)
- ✅ 未硬编码密钥
- ⚠️ 默认值过弱

### NIST 密钥管理标准
- ⚠️ 密钥长度应 >= 256 bits (当前未验证)
- ⚠️ 密钥应定期轮换 (当前不支持)
- ⚠️ 密钥应随机生成 (当前依赖用户手动设置)

## 四、对比主流框架

### Spring Security (Java)
```java
// 强制设置密钥，无默认值
@Value("${jwt.secret}")
private String jwtSecret;

// 启动时验证
@PostConstruct
public void validateConfig() {
    if (jwtSecret.length() < 32) {
        throw new IllegalStateException("JWT secret too short");
    }
}
```

### Django REST Framework (Python)
```python
# settings.py
if not SECRET_KEY or SECRET_KEY == 'change-me':
    raise ImproperlyConfigured("SECRET_KEY must be set")

if len(SECRET_KEY) < 50:
    warnings.warn("SECRET_KEY should be at least 50 characters")
```

### Express.js + Passport (Node.js)
```javascript
if (!process.env.JWT_SECRET) {
  throw new Error('JWT_SECRET environment variable is required');
}

if (process.env.JWT_SECRET.length < 32) {
  console.error('WARNING: JWT_SECRET should be at least 32 characters');
}
```

## 五、修复建议（优先级排序）

### P0 - 立即修复（安全漏洞）

#### 1. 移除弱默认值，强制设置
```go
// system/backend/internal/config/config.go
func Load() *Config {
    jwtSecret := getEnv("JWT_SECRET", "")
    if jwtSecret == "" {
        log.Fatal("FATAL: JWT_SECRET must be set!\n" +
            "Generate with: openssl rand -base64 64\n" +
            "Or: go run -ldflags=\"-s -w\" crypto/rand")
    }

    if len(jwtSecret) < 32 {
        log.Fatal("FATAL: JWT_SECRET must be at least 32 characters (256 bits)")
    }

    env := getEnv("ENV", "development")
    if env == "production" && strings.Contains(jwtSecret, "change-this") {
        log.Fatal("FATAL: Default JWT_SECRET detected in production!")
    }

    cfg := &Config{
        JWTSecret: jwtSecret,
        Env:       env,
        // ...
    }
    return cfg
}
```

#### 2. 验证签名算法
```go
// system/backend/pkg/utils/jwt.go
func ParseToken(tokenString string, secret string) (*Claims, error) {
    token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
        // CRITICAL: 验证签名算法，防止算法降级攻击
        if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
            return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
        }
        if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
            return nil, fmt.Errorf("unexpected signing method: %v", token.Method.Alg())
        }
        return []byte(secret), nil
    })

    if err != nil {
        return nil, err
    }

    if claims, ok := token.Claims.(*Claims); ok && token.Valid {
        return claims, nil
    }

    return nil, jwt.ErrSignatureInvalid
}
```

### P1 - 近期修复（安全增强）

#### 3. 实现 Token 黑名单
```go
// system/backend/internal/middleware/auth.go
func AuthMiddleware(redis *redis.Client, cfg *config.Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        // ... 解析 Token

        // 检查 Token 是否在黑名单
        blacklistKey := fmt.Sprintf("token:blacklist:%d", claims.UserID)
        if redis.Exists(c, blacklistKey).Val() > 0 {
            c.JSON(401, gin.H{"error": "token has been revoked"})
            c.Abort()
            return
        }

        // ...
    }
}

// 登出时加入黑名单
func (h *AuthHandler) Logout(c *gin.Context) {
    userID := c.GetUint("user_id")
    blacklistKey := fmt.Sprintf("token:blacklist:%d", userID)

    // 设置过期时间与 Token 一致
    h.redis.Set(c, blacklistKey, "1", 30*time.Minute)

    c.JSON(200, gin.H{"message": "logged out successfully"})
}
```

#### 4. 密钥轮换支持
```go
// system/backend/internal/config/config.go
type Config struct {
    JWTSecret    string   // 当前密钥（用于签名）
    JWTSecretOld string   // 旧密钥（仅用于验证）
}

// system/backend/pkg/utils/jwt.go
func ParseTokenWithFallback(tokenString string, secrets []string) (*Claims, error) {
    var lastErr error

    for _, secret := range secrets {
        claims, err := ParseToken(tokenString, secret)
        if err == nil {
            return claims, nil
        }
        lastErr = err
    }

    return nil, lastErr
}
```

### P2 - 计划修复（用户体验）

#### 5. 改进 .env.example 文档
```bash
# .env.example
# ==================== System 模块配置 ====================

# JWT Secret（用于签名认证 Token）
# ⚠️ 生产环境必须修改！建议至少 64 字符（512 bits）
# 生成方法：
#   方法1: openssl rand -base64 64
#   方法2: node -e "console.log(require('crypto').randomBytes(64).toString('base64'))"
#   方法3: python3 -c "import secrets; print(secrets.token_urlsafe(64))"
# 注意：切勿使用字典词汇或简单字符串！
JWT_SECRET=your-super-secret-jwt-key-change-this-in-production

# 内部服务调用 API Key（用于服务间认证）
# 生成方法同上
INTERNAL_API_KEY=dev-internal-key
```

### P3 - 优化项（长期规划）

#### 6. Refresh Token 机制
```go
// 双 Token 机制
type TokenPair struct {
    AccessToken  string `json:"access_token"`   // 15 分钟
    RefreshToken string `json:"refresh_token"`  // 7 天
}

func (h *AuthHandler) Login(c *gin.Context) {
    // ... 验证用户

    // 生成 Access Token（短期）
    accessToken, _ := utils.GenerateToken(user.ID, user.Username, cfg.JWTSecret, 15)

    // 生成 Refresh Token（长期），存储到 Redis
    refreshToken := generateRefreshToken()
    redis.Set(c, fmt.Sprintf("refresh:%s", refreshToken), user.ID, 7*24*time.Hour)

    c.JSON(200, TokenPair{
        AccessToken:  accessToken,
        RefreshToken: refreshToken,
    })
}

func (h *AuthHandler) RefreshToken(c *gin.Context) {
    refreshToken := c.PostForm("refresh_token")

    // 从 Redis 验证 Refresh Token
    userID, err := redis.Get(c, fmt.Sprintf("refresh:%s", refreshToken)).Uint64()
    if err != nil {
        c.JSON(401, gin.H{"error": "invalid refresh token"})
        return
    }

    // 生成新的 Access Token
    user := h.userRepo.GetByID(uint(userID))
    accessToken, _ := utils.GenerateToken(user.ID, user.Username, cfg.JWTSecret, 15)

    c.JSON(200, gin.H{"access_token": accessToken})
}
```

## 六、检查清单

### 开发环境
- [ ] JWT_SECRET 已设置（长度 >= 32）
- [ ] .env 已加入 .gitignore
- [ ] .env.example 包含生成指导

### 生产环境部署前
- [ ] JWT_SECRET 使用强随机密钥（>= 64 字符）
- [ ] 验证 ENV=production 时未使用默认密钥
- [ ] Token 过期时间合理（建议 15-30 分钟）
- [ ] 配置 Refresh Token（可选）
- [ ] 启用 Token 黑名单（Redis）
- [ ] 启用 HTTPS（防止 Token 被窃听）
- [ ] 配置 CORS 白名单
- [ ] 启用访问日志和审计

### 代码审查
- [ ] ParseToken 验证签名算法
- [ ] 密钥不允许使用空值或默认值
- [ ] Token 验证包含过期时间检查
- [ ] 密钥从环境变量读取，未硬编码

## 七、总结

### 当前安全等级：⚠️ **MEDIUM-HIGH RISK**

虽然基本架构正确（环境变量、.gitignore、标准算法），但存在以下关键安全隐患：

1. **弱默认密钥** - 可被暴力破解
2. **未验证签名算法** - 可能遭受算法降级攻击（CVE-2015-9235）
3. **无 Token 撤销机制** - 无法应对密钥泄露

### 建议行动

**立即修复（本周内）**：
1. 实现密钥强度验证（<32 字符拒绝启动）
2. 实现签名算法白名单验证
3. 更新 .env.example 添加密钥生成指导

**近期修复（本月内）**：
4. 实现 Token 黑名单（Redis）
5. 支持密钥轮换
6. 添加生产环境密钥检查

**长期优化（下季度）**：
7. 实现 Refresh Token 机制
8. 添加安全审计日志
9. 集成密钥管理服务（如 Vault）

## 八、参考资料

- [OWASP JWT Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/JSON_Web_Token_for_Java_Cheat_Sheet.html)
- [RFC 7519 - JSON Web Token](https://datatracker.ietf.org/doc/html/rfc7519)
- [CWE-798: Use of Hard-coded Credentials](https://cwe.mitre.org/data/definitions/798.html)
- [CVE-2015-9235: JWT Algorithm Confusion](https://nvd.nist.gov/vuln/detail/CVE-2015-9235)
- [NIST SP 800-57: Key Management](https://csrc.nist.gov/publications/detail/sp/800-57-part-1/rev-5/final)
