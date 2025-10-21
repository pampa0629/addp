# Transfer 模块 - 内部 API 认证配置指南

**版本**: v1.2.0
**日期**: 2025-10-21
**状态**: ✅ 已实现

---

## 📌 概述

Transfer 模块现已实现完整的内部 API Key 认证机制，用于与 System 模块进行安全的服务间通信。这确保了只有经过授权的服务才能访问 System 的内部 API 获取资源配置。

---

## 🔐 认证机制

### 认证类型对比

| 认证类型 | 用途 | 认证方式 | 适用场景 |
|---------|------|---------|---------|
| **JWT Token** | 用户认证 | `Authorization: Bearer <jwt>` | 前端调用 API |
| **Internal API Key** | 服务间认证 | `X-Internal-API-Key: <key>` | 后端服务调用 |

### 架构图

```
┌─────────────────────────────────────────────────────┐
│  Transfer 模块                                       │
│                                                     │
│  ┌────────────────────────────────────────────┐   │
│  │  TaskService                               │   │
│  │                                             │   │
│  │  systemClient = NewSystemClientWithInternalKey( │
│  │    systemURL,                              │   │
│  │    cfg.InternalAPIKey  ← 从环境变量读取    │   │
│  │  )                                          │   │
│  └────────────────────────────────────────────┘   │
│                    ↓                                │
│         HTTP Request with Header:                  │
│         X-Internal-API-Key: dev-internal-key       │
│                    ↓                                │
└────────────────────┼────────────────────────────────┘
                     │
                     ↓
┌────────────────────────────────────────────────────┐
│  System 模块                                        │
│                                                     │
│  ┌────────────────────────────────────────────┐   │
│  │  Internal API Middleware                   │   │
│  │                                             │   │
│  │  func InternalAPIKeyAuth() {               │   │
│  │    key := c.GetHeader("X-Internal-API-Key")│   │
│  │    if key != cfg.InternalAPIKey {          │   │
│  │      return 401 Unauthorized               │   │
│  │    }                                        │   │
│  │    c.Next()                                │   │
│  │  }                                          │   │
│  └────────────────────────────────────────────┘   │
│                    ↓                                │
│  ┌────────────────────────────────────────────┐   │
│  │  /internal/resources/:id                   │   │
│  │  返回资源配置（包含解密后的密码）            │   │
│  └────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────┘
```

---

## 🛠️ 实现细节

### 1. Transfer 模块配置

#### config.go 修改

```go
// transfer/backend/internal/config/config.go
type Config struct {
    commonConfig.BaseConfig

    Port            string
    DBSchema        string
    InternalAPIKey  string  // ← 新增字段
    // ... 其他配置
}

func Load() *Config {
    cfg := &Config{
        Port:           commonConfig.GetEnv("PORT", "8083"),
        DBSchema:       commonConfig.GetEnv("DB_SCHEMA", "transfer"),
        InternalAPIKey: commonConfig.GetEnv("INTERNAL_API_KEY", ""),  // ← 读取环境变量
        // ... 其他配置
    }

    // 从 System 获取共享配置
    if cfg.EnableIntegration {
        commonConfig.LoadSharedConfig(systemURL, &cfg.BaseConfig)
    }

    // 如果本地未配置，尝试从 BaseConfig 获取
    if cfg.InternalAPIKey == "" {
        cfg.InternalAPIKey = cfg.BaseConfig.InternalAPIKey  // ← Fallback
    }

    return cfg
}
```

### 2. TaskService 使用内部认证

#### task_service.go 修改

```go
// transfer/backend/internal/service/task_service.go
func NewTaskService(
    db *gorm.DB,
    engine *pipeline.ExecutionEngine,
    cfg *config.Config,
) *TaskService {
    var systemClient *commonClient.SystemClient

    if cfg.EnableIntegration && cfg.SystemServiceURL != "" {
        if cfg.InternalAPIKey != "" {
            // ✅ 使用内部 API Key（推荐）
            systemClient = commonClient.NewSystemClientWithInternalKey(
                cfg.SystemServiceURL,
                cfg.InternalAPIKey,
            )
            slog.Info("SystemClient initialized with internal API key")
        } else {
            // ⚠️ Fallback：无认证（仅开发环境）
            systemClient = commonClient.NewSystemClient(cfg.SystemServiceURL, "")
            slog.Warn("SystemClient initialized without authentication")
        }
    }

    return &TaskService{
        // ...
        systemClient: systemClient,
    }
}
```

### 3. SystemClient 实现

#### common/client/system.go

```go
// SystemClient 支持两种认证方式
type SystemClient struct {
    baseURL     string
    httpClient  *http.Client
    authToken   string     // JWT Token（用户认证）
    internalKey string     // Internal API Key（服务间调用）
}

// 方式1：用户认证
func NewSystemClient(baseURL, authToken string) *SystemClient {
    return &SystemClient{
        baseURL:   baseURL,
        authToken: authToken,
        // ...
    }
}

// 方式2：服务间认证（推荐）
func NewSystemClientWithInternalKey(baseURL, internalKey string) *SystemClient {
    return &SystemClient{
        baseURL:     baseURL,
        internalKey: internalKey,  // ← 使用内部 Key
        // ...
    }
}

// 添加认证头
func (c *SystemClient) addAuth(req *http.Request) {
    if c.internalKey != "" {
        // 服务间调用使用 Internal API Key
        req.Header.Set("X-Internal-API-Key", c.internalKey)
    } else if c.authToken != "" {
        // 用户调用使用 JWT Token
        req.Header.Set("Authorization", "Bearer "+c.authToken)
    }
}

// 获取资源（自动选择正确的 endpoint）
func (c *SystemClient) GetResource(resourceID uint) (*models.Resource, error) {
    var url string
    if c.internalKey != "" {
        // 使用内部 API
        url = fmt.Sprintf("%s/internal/resources/%d", c.baseURL, resourceID)
    } else {
        // 使用公开 API
        url = fmt.Sprintf("%s/api/resources/%d", c.baseURL, resourceID)
    }

    req, _ := http.NewRequest("GET", url, nil)
    c.addAuth(req)  // ← 添加认证头
    // ... 发送请求
}
```

---

## ⚙️ 配置指南

### 环境变量配置

#### 1. 项目根目录 `.env`

```bash
# 全局内部 API Key（所有服务共享）
INTERNAL_API_KEY=your-production-internal-key-change-this

# 或使用强随机生成
# openssl rand -base64 32
INTERNAL_API_KEY=XyZ9aBc7DeFgHiJkLmNoPqRsTuVwXyZ1234567890==
```

#### 2. Transfer 模块 `backend/.env`

```bash
# Transfer 服务配置
PORT=8083
DB_SCHEMA=transfer

# 服务集成配置
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true

# 内部 API Key（必须与 System 一致）
INTERNAL_API_KEY=your-production-internal-key-change-this
```

#### 3. System 模块配置（参考）

```bash
# system/backend/.env
INTERNAL_API_KEY=your-production-internal-key-change-this
```

**⚠️ 重要**: 所有服务（System, Manager, Meta, Transfer）的 `INTERNAL_API_KEY` 必须完全一致！

---

## 🔑 密钥生成

### 推荐方法

```bash
# 方法 1: OpenSSL (推荐，跨平台)
openssl rand -base64 32

# 方法 2: Python
python3 -c "import secrets; print(secrets.token_urlsafe(32))"

# 方法 3: Node.js
node -e "console.log(require('crypto').randomBytes(32).toString('base64'))"

# 示例输出
XyZ9aBc7DeFgHiJkLmNoPqRsTuVwXyZ1234567890==
```

### 密钥要求

- ✅ **长度**: 至少 32 字符（256 bits）
- ✅ **随机性**: 使用加密安全的随机数生成器
- ✅ **唯一性**: 与 JWT_SECRET 使用不同的密钥
- ❌ **禁止**: 使用字典词汇、简单字符串、默认值

---

## 🚀 部署流程

### 开发环境

```bash
# 1. 生成内部 API Key
export INTERNAL_API_KEY=$(openssl rand -base64 32)

# 2. 配置到所有服务
echo "INTERNAL_API_KEY=$INTERNAL_API_KEY" >> .env

# 3. 启动服务
make dev-start
```

### Docker 部署

#### docker-compose.yml

```yaml
services:
  transfer-backend:
    build: ./transfer/backend
    environment:
      - PORT=8083
      - SYSTEM_SERVICE_URL=http://system-backend:8080
      - ENABLE_SERVICE_INTEGRATION=true
      - INTERNAL_API_KEY=${INTERNAL_API_KEY}  # ← 从 .env 读取
    networks:
      - addp-network
```

### 生产环境（Kubernetes）

```yaml
# secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: addp-internal-keys
type: Opaque
data:
  internal-api-key: <base64-encoded-key>

---
# transfer-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: transfer-backend
spec:
  template:
    spec:
      containers:
      - name: transfer
        image: transfer-backend:latest
        env:
        - name: INTERNAL_API_KEY
          valueFrom:
            secretKeyRef:
              name: addp-internal-keys
              key: internal-api-key
```

---

## 🧪 测试验证

### 1. 验证配置是否生效

```bash
# 启动 Transfer 服务，查看日志
cd transfer/backend
go run cmd/server/main.go

# 期望输出
✅ Successfully loaded shared config from System service
INFO SystemClient initialized with internal API key system_url=http://localhost:8080
```

**如果看到警告**:
```
WARN SystemClient initialized without authentication - not recommended for production
```
说明 `INTERNAL_API_KEY` 未配置，请检查环境变量。

### 2. 测试资源获取

```bash
# 创建任务使用 resource_id
curl -X POST http://localhost:8083/api/tasks \
  -H "Authorization: Bearer <user-jwt-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test Task",
    "source_id": 10,
    "config": {
      "source": {"query": "SELECT 1"}
    }
  }'

# 查看 Transfer 日志
# 期望输出
INFO fetching resource config from System resource_id=10
INFO resource config fetched successfully resource_id=10 resource_type=postgresql
```

### 3. 测试内部 API 调用

```bash
# 直接调用 System 内部 API（模拟 Transfer 行为）
curl http://localhost:8080/internal/resources/10 \
  -H "X-Internal-API-Key: your-internal-key"

# 成功响应（200）
{
  "id": 10,
  "name": "Production DB",
  "resource_type": "postgresql",
  "connection_info": {
    "host": "prod-db.example.com",
    "password": "decrypted_password"  # ← 已解密
  }
}

# 失败响应（401）- 密钥错误
{
  "error": "Unauthorized"
}
```

---

## 🐛 故障排查

### 问题 1: SystemClient 未使用认证

**日志**:
```
WARN SystemClient initialized without authentication
```

**原因**: `INTERNAL_API_KEY` 环境变量未设置

**解决方法**:
```bash
# 检查环境变量
echo $INTERNAL_API_KEY

# 如果为空，设置环境变量
export INTERNAL_API_KEY=your-key-here

# 或在 .env 文件中添加
echo "INTERNAL_API_KEY=your-key-here" >> transfer/backend/.env

# 重启服务
make dev-restart
```

### 问题 2: 获取资源失败（401 Unauthorized）

**日志**:
```
ERROR failed to get resource from System
error=system api returned status 401: Unauthorized
```

**原因**: 内部 API Key 不匹配

**解决方法**:
```bash
# 1. 确认 System 的 INTERNAL_API_KEY
grep INTERNAL_API_KEY system/backend/.env

# 2. 确认 Transfer 的 INTERNAL_API_KEY
grep INTERNAL_API_KEY transfer/backend/.env

# 3. 确保两者完全一致
# System: INTERNAL_API_KEY=abc123
# Transfer: INTERNAL_API_KEY=abc123  ← 必须相同

# 4. 重启所有服务
make dev-restart
```

### 问题 3: System 内部 API 未启用

**日志**:
```
ERROR system api returned status 404: Not Found
```

**原因**: System 模块的 `/internal/resources` 接口未实现或未启用

**解决方法**:

检查 System 模块是否实现了内部 API：

```go
// system/backend/internal/api/router.go
func SetupRouter() *gin.Engine {
    // ...

    // 内部 API（需要内部认证）
    internal := r.Group("/internal")
    internal.Use(middleware.InternalAPIKeyAuth())
    {
        internal.GET("/resources/:id", resourceHandler.GetInternal)
        internal.GET("/resources", resourceHandler.ListInternal)
    }

    return r
}
```

如果未实现，参考 Meta 模块的实现。

### 问题 4: 配置未生效

**日志**:
```
INFO system client not available (integration disabled)
```

**原因**: `ENABLE_SERVICE_INTEGRATION` 设置为 false

**解决方法**:
```bash
# 启用服务集成
export ENABLE_SERVICE_INTEGRATION=true

# 或在 .env 中设置
echo "ENABLE_SERVICE_INTEGRATION=true" >> transfer/backend/.env
```

---

## 🔒 安全最佳实践

### 1. 密钥管理

✅ **推荐做法**:
- 使用环境变量或密钥管理服务（Vault, AWS Secrets Manager）
- 生产环境使用强随机生成的密钥（至少 32 字符）
- 定期轮换密钥（建议每 90 天）
- 密钥泄露后立即更换

❌ **禁止做法**:
- 硬编码在代码中
- 提交到 Git 仓库
- 使用弱密钥（如 "admin", "123456"）
- 在日志中打印完整密钥

### 2. 网络隔离

```yaml
# docker-compose.yml
services:
  transfer-backend:
    networks:
      - internal-network  # ← 仅内部网络

networks:
  internal-network:
    internal: true  # ← 不暴露到外部
```

### 3. 日志脱敏

```go
// 正确：不打印完整密钥
slog.Info("Internal API key configured", "key_prefix", key[:8]+"...")

// 错误：泄露完整密钥
slog.Info("Using key", "key", cfg.InternalAPIKey)  // ❌
```

### 4. 监控和告警

**监控指标**:
- 内部 API 调用成功率
- 认证失败次数（异常时告警）
- 响应时间

**告警规则**:
```
内部API认证失败率 > 5% → 发送告警
连续 10 次认证失败 → 可能被攻击
```

---

## 📊 对比：认证前 vs 认证后

| 维度 | 认证前 (v1.1.0) | 认证后 (v1.2.0) |
|------|----------------|----------------|
| **安全性** | ⚠️ 无认证，任何服务可调用 | ✅ 需要密钥，防止未授权访问 |
| **审计能力** | ❌ 无法区分调用来源 | ✅ 可追踪哪个服务调用 |
| **生产就绪** | ❌ 不推荐 | ✅ 推荐 |
| **配置复杂度** | ✅ 简单 | ⚠️ 需要配置密钥 |
| **性能影响** | - | 可忽略（仅验证字符串） |

---

## 📚 相关文档

- [SystemClient集成指南.md](SystemClient集成指南.md) - 资源配置集成
- [修复日志.md](修复日志.md) - 修复记录
- [backend/.env.example](backend/.env.example) - 配置模板
- [common/client/system.go](../common/client/system.go) - SystemClient 实现

---

## 📝 版本历史

### v1.2.0 (2025-10-21)

**新增**:
- ✅ 内部 API Key 认证
- ✅ Config 中添加 InternalAPIKey 字段
- ✅ TaskService 使用认证初始化 SystemClient
- ✅ 完整的配置文件和文档

**改进**:
- ✅ 安全性提升：防止未授权的服务间调用
- ✅ 可观测性提升：日志区分认证方式
- ✅ 生产就绪：符合安全规范

---

**更新时间**: 2025-10-21
**维护者**: ADDP Transfer Team
**审核状态**: ✅ 已验证
