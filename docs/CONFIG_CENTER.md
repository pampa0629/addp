# 配置中心使用指南 (Configuration Center Guide)

本文档详细说明 ADDP 平台的配置中心架构及使用方法。

## 📋 目录

1. [架构概述](#架构概述)
2. [核心概念](#核心概念)
3. [配置项分类](#配置项分类)
4. [使用指南](#使用指南)
5. [实施细节](#实施细节)
6. [故障排查](#故障排查)
7. [最佳实践](#最佳实践)

---

## 架构概述

### 设计原则

**System 模块作为全平台唯一的配置中心**，所有其他模块（Manager、Meta、Transfer）在启动时从 System 获取共享配置。

### 架构图

```
┌─────────────────────────────────────────────────────────────┐
│              System Module (Configuration Center)           │
│                                                             │
│  ┌────────────────────────────────────────────────────┐   │
│  │  System Backend (.env + PostgreSQL)                │   │
│  │                                                      │   │
│  │  环境变量配置:                                        │   │
│  │  - JWT_SECRET=xxx                                   │   │
│  │  - POSTGRES_HOST=localhost                          │   │
│  │  - POSTGRES_PORT=5432                               │   │
│  │  - POSTGRES_USER=addp                               │   │
│  │  - POSTGRES_PASSWORD=xxx                            │   │
│  │  - POSTGRES_DB=addp                                 │   │
│  │  - ENCRYPTION_KEY=<base64>                          │   │
│  │                                                      │   │
│  │  内部 API:                                           │   │
│  │  GET /internal/config                               │   │
│  │  └─ 返回共享配置给其他模块                            │   │
│  │                                                      │   │
│  │  公开 API:                                           │   │
│  │  GET /api/resources                                 │   │
│  │  └─ 管理业务数据库配置 (加密存储)                      │   │
│  └────────────────────────────────────────────────────┘   │
└────────────────┬────────────────────────────────────────────┘
                 │
                 │ HTTP Request (启动时)
                 │
      ┌──────────┼─────────────┬─────────────┐
      ▼          ▼             ▼             ▼
  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌──────────┐
  │ Manager │ │  Meta   │ │Transfer │ │ Gateway  │
  │         │ │         │ │         │ │          │
  │ 8081    │ │ 8082    │ │ 8083    │ │ 8000     │
  └─────────┘ └─────────┘ └─────────┘ └──────────┘
      │            │            │            │
      └────────────┴────────────┴────────────┘
                   │
                   ▼
         共享 PostgreSQL 数据库
         (manager/metadata/transfer schemas)
```

---

## 核心概念

### 1. 配置中心 (Configuration Center)

System 模块提供两个层次的配置管理：

#### **层次一：系统配置 (`/internal/config` API)**

提供给其他模块的系统级共享配置：

```json
{
  "jwt_secret": "your-super-secret-jwt-key",
  "database": {
    "host": "localhost",
    "port": "5432",
    "user": "addp",
    "password": "addp_password",
    "name": "addp"
  },
  "encryption_key": "ZGV2LWVuY3J5cHRpb24ta2V5LTMyLWJ5dGVzIQ=="
}
```

#### **层次二：业务数据库配置 (`/api/resources` API)**

管理所有业务数据源的连接信息：

```json
{
  "id": 1,
  "name": "业务MySQL数据库",
  "resource_type": "mysql",
  "connection_info": {
    "host": "business-mysql.example.com",
    "port": "3306",
    "user": "business_user",
    "password": "***encrypted***",  // 自动加密存储
    "database": "business_db"
  }
}
```

### 2. 配置消费者 (Configuration Consumers)

Manager、Meta、Transfer 模块在启动时：

1. 调用 System 的 `/internal/config` 获取系统配置
2. 使用 SystemClient 从 `/api/resources` 获取业务数据库配置
3. 如果 System 不可用，降级到本地 `.env` 配置

### 3. 降级机制 (Fallback Mechanism)

当 System 服务不可用时，各模块自动使用本地 `.env` 文件中的备用配置。

---

## 配置项分类

### ✅ 集中管理的配置（在 System 中配置）

| 配置项 | 说明 | 存储位置 |
|--------|------|----------|
| `JWT_SECRET` | JWT 签名密钥，所有服务必须一致 | System `.env` |
| `POSTGRES_HOST` | PostgreSQL 主机地址 | System `.env` |
| `POSTGRES_PORT` | PostgreSQL 端口 | System `.env` |
| `POSTGRES_USER` | PostgreSQL 用户名 | System `.env` |
| `POSTGRES_PASSWORD` | PostgreSQL 密码 | System `.env` |
| `POSTGRES_DB` | PostgreSQL 数据库名 | System `.env` |
| `ENCRYPTION_KEY` | AES-256 加密密钥 | System `.env` |
| 业务数据源配置 | MySQL、PostgreSQL、MongoDB 等 | System `resources` 表 |

### ✅ 模块特有配置（在各模块中配置）

| 配置项 | 说明 | 配置位置 |
|--------|------|----------|
| `PORT` | 各模块自己的端口号 | 各模块 `.env` |
| `DB_SCHEMA` | 各模块的 PostgreSQL schema | 各模块 `.env` |
| `SYSTEM_SERVICE_URL` | System 服务地址 | 各模块 `.env` |
| `ENABLE_SERVICE_INTEGRATION` | 是否启用配置中心 | 各模块 `.env` |
| 模块特有功能配置 | 如 Meta 的同步配置、Transfer 的任务配置 | 各模块 `.env` |

---

## 使用指南

### 场景 1: 全新部署

#### 步骤 1: 配置 System 模块

创建或编辑 `/Users/zengzhiming/code/addp/.env`（项目根目录）：

```bash
# 安全配置（生产环境必须修改）
JWT_SECRET=your-super-secret-jwt-key-change-this-in-production

# PostgreSQL 配置（所有模块共享）
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_USER=addp
POSTGRES_PASSWORD=addp_password
POSTGRES_DB=addp

# Redis 配置
REDIS_PASSWORD=addp_redis

# MinIO 配置
MINIO_ROOT_PASSWORD=minioadmin

# 加密密钥（Base64 编码的 32 字节密钥）
# 生成方式: openssl rand -base64 32
ENCRYPTION_KEY=<your-base64-encoded-32-byte-key>

# 服务集成开关
ENABLE_SERVICE_INTEGRATION=true

# 可选：内部 API 保护
INTERNAL_API_KEY=your-internal-api-key-for-service-to-service
```

#### 步骤 2: 启动 System 模块

```bash
cd system/backend
go run cmd/server/main.go
```

System 启动后会：
- 读取 `.env` 配置
- 连接 PostgreSQL
- 提供 `/internal/config` API

#### 步骤 3: 配置其他模块

**Manager 模块** (`manager/backend/.env`):

```bash
PORT=8081
DB_SCHEMA=manager
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true

# 共享配置自动从 System 获取，无需配置：
# - JWT_SECRET
# - POSTGRES_HOST/PORT/USER/PASSWORD/DB
# - ENCRYPTION_KEY
```

**Meta 模块** (`meta/backend/.env`):

```bash
PORT=8082
DB_SCHEMA=metadata
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true

# Meta 特有配置
AUTO_SYNC_ENABLED=true
AUTO_SYNC_SCHEDULE=0 0 * * *
AUTO_SYNC_LEVEL=database
```

**Transfer 模块** (`transfer/backend/.env`):

```bash
PORT=8083
DB_SCHEMA=transfer
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true

# Transfer 特有配置
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=addp_redis
WORKER_COUNT=5
```

#### 步骤 4: 启动其他模块

```bash
# Terminal 1: Manager
cd manager/backend && go run cmd/server/main.go

# Terminal 2: Meta
cd meta/backend && go run cmd/server/main.go

# Terminal 3: Transfer
cd transfer/backend && go run cmd/server/main.go
```

启动日志会显示：
```
🔄 Attempting to load shared config from System service...
✅ Successfully loaded shared config from System service
```

---

### 场景 2: 修改数据库密码

只需修改一处，重启所有服务即可生效。

#### 步骤 1: 修改 System 配置

编辑项目根目录 `.env`:

```bash
POSTGRES_PASSWORD=new_password_here
```

#### 步骤 2: 重启所有服务

```bash
# 方式 1: 使用 Makefile
make restart-full

# 方式 2: 手动重启
pkill -f "go run cmd/server/main.go"
# 然后逐个启动各模块
```

所有模块会自动从 System 获取新密码。

---

### 场景 3: 添加业务数据库

在 System 中创建资源配置，其他模块通过 SystemClient 获取。

#### 步骤 1: 在 System 创建资源

```bash
curl -X POST http://localhost:8080/api/resources \
  -H "Authorization: Bearer <your-jwt-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "业务MySQL数据库",
    "resource_type": "mysql",
    "connection_info": {
      "host": "business-db.example.com",
      "port": "3306",
      "user": "business_user",
      "password": "business_pass",
      "database": "business_db"
    },
    "description": "生产环境业务数据库"
  }'
```

System 会自动加密 `password` 字段。

#### 步骤 2: 在其他模块中使用

```go
// Manager/Meta/Transfer 中使用 SystemClient
import "github.com/addp/manager/pkg/utils"

// 创建客户端
client := utils.NewSystemClient(systemURL, jwtToken)

// 获取资源（password 自动解密）
resource, err := client.GetResource(resourceID)
if err != nil {
    return err
}

// 构建连接字符串
connStr, err := utils.BuildConnectionString(resource)
// 返回: "business_user:business_pass@tcp(business-db.example.com:3306)/business_db?parseTime=true"

// 使用连接字符串
db, err := gorm.Open(mysql.Open(connStr), &gorm.Config{})
```

---

### 场景 4: 独立部署（不使用配置中心）

某些场景下，可能需要独立部署某个模块。

#### 修改模块 .env

```bash
# 禁用服务集成
ENABLE_SERVICE_INTEGRATION=false

# 配置本地数据库连接
DB_HOST=localhost
DB_PORT=5432
DB_USER=addp
DB_PASSWORD=addp_password
DB_NAME=addp

# 配置本地 JWT 密钥
JWT_SECRET=your-local-jwt-secret

# 配置本地加密密钥
ENCRYPTION_KEY=<base64-encoded-key>
```

模块启动时会显示：
```
ℹ️  Service integration disabled, using local config
```

---

## 实施细节

### System 模块实现

#### 1. 配置 API Handler

文件：`system/backend/internal/api/config_handler.go`

```go
func (h *ConfigHandler) GetSharedConfig(c *gin.Context) {
    // 可选：验证内部 API Key
    apiKey := c.GetHeader("X-Internal-API-Key")
    if expectedKey != "" && apiKey != expectedKey {
        c.JSON(401, gin.H{"error": "unauthorized"})
        return
    }

    // 返回共享配置
    c.JSON(200, gin.H{
        "jwt_secret": h.cfg.JWTSecret,
        "database": gin.H{
            "host":     h.cfg.PostgresHost,
            "port":     h.cfg.PostgresPort,
            "user":     h.cfg.PostgresUser,
            "password": h.cfg.PostgresPassword,
            "name":     h.cfg.PostgresDB,
        },
        "encryption_key": h.cfg.EncryptionKey,
    })
}
```

#### 2. 路由注册

文件：`system/backend/internal/api/router.go`

```go
// 内部 API（用于服务间调用）
internal := router.Group("/internal")
{
    configHandler := NewConfigHandler(cfg)
    internal.GET("/config", configHandler.GetSharedConfig)
}
```

### 消费者模块实现

#### 1. 配置加载逻辑

文件：`manager/backend/internal/config/config.go`、`meta/backend/internal/config/config.go`

```go
func Load() *Config {
    systemURL := getEnv("SYSTEM_SERVICE_URL", "http://localhost:8080")
    cfg := &Config{...}

    if cfg.EnableIntegration {
        log.Println("🔄 Attempting to load shared config from System service...")
        if err := cfg.loadSharedConfig(systemURL); err != nil {
            log.Printf("⚠️  Warning: Failed to load shared config from System: %v", err)
            log.Printf("⚠️  Falling back to local environment variables...")
            cfg.loadLocalConfig()
        } else {
            log.Println("✅ Successfully loaded shared config from System service")
        }
    } else {
        log.Println("ℹ️  Service integration disabled, using local config")
        cfg.loadLocalConfig()
    }

    return cfg
}
```

#### 2. SystemClient 实现

文件：`manager/backend/pkg/utils/system_client.go`

```go
type SystemClient struct {
    baseURL    string
    httpClient *http.Client
    authToken  string
}

func (c *SystemClient) GetResource(resourceID uint) (*Resource, error) {
    url := fmt.Sprintf("%s/api/resources/%d", c.baseURL, resourceID)
    req, _ := http.NewRequest("GET", url, nil)
    req.Header.Set("Authorization", "Bearer "+c.authToken)

    resp, err := c.httpClient.Do(req)
    // ... 处理响应

    var resource Resource
    json.NewDecoder(resp.Body).Decode(&resource)
    return &resource, nil
}
```

---

## 故障排查

### 问题 1: 模块启动失败，显示"Failed to load shared config"

**症状**：
```
⚠️  Warning: Failed to load shared config from System: failed to connect to System service: ...
⚠️  Falling back to local environment variables...
```

**原因**：
- System 服务未启动
- System 服务地址配置错误
- 网络连接问题

**解决方案**：

1. 检查 System 是否运行：
   ```bash
   curl http://localhost:8080/health
   ```

2. 检查 `SYSTEM_SERVICE_URL` 配置：
   ```bash
   # 在模块 .env 中
   SYSTEM_SERVICE_URL=http://localhost:8080  # 确保正确
   ```

3. 检查防火墙/网络：
   ```bash
   telnet localhost 8080
   ```

### 问题 2: JWT 认证失败

**症状**：
```
401 Unauthorized: invalid token signature
```

**原因**：
各模块的 `JWT_SECRET` 不一致。

**解决方案**：

1. 确保所有模块启用了配置集成：
   ```bash
   ENABLE_SERVICE_INTEGRATION=true
   ```

2. 重启所有服务确保加载最新配置：
   ```bash
   make restart-full
   ```

3. 验证 System 返回的配置：
   ```bash
   curl http://localhost:8080/internal/config
   ```

### 问题 3: 数据库连接失败

**症状**：
```
Error: failed to connect to database
```

**解决方案**：

1. 检查 System 配置中的数据库信息：
   ```bash
   # 项目根目录 .env
   POSTGRES_HOST=localhost
   POSTGRES_PORT=5432
   POSTGRES_USER=addp
   POSTGRES_PASSWORD=addp_password
   POSTGRES_DB=addp
   ```

2. 测试数据库连接：
   ```bash
   psql -h localhost -p 5432 -U addp -d addp
   ```

3. 检查模块日志确认配置加载：
   ```bash
   # Meta 模块日志应显示
   ✅ Successfully loaded shared config from System service
   ```

### 问题 4: 内部 API 返回 401

**症状**：
```
system api returned status 401: unauthorized: invalid internal API key
```

**原因**：
设置了 `INTERNAL_API_KEY` 但模块没有配置。

**解决方案**：

在所有模块的 `.env` 中添加相同的 API Key：

```bash
# System .env
INTERNAL_API_KEY=your-secret-api-key

# Manager/Meta/Transfer .env
INTERNAL_API_KEY=your-secret-api-key
```

---

## 最佳实践

### 1. 安全性

✅ **生产环境必须修改默认密钥**：
```bash
# 生成安全的 JWT Secret
openssl rand -base64 64

# 生成 32 字节加密密钥
openssl rand -base64 32

# 生成内部 API Key
openssl rand -base64 32
```

✅ **使用内部 API Key 保护配置接口**：
```bash
# System .env
INTERNAL_API_KEY=$(openssl rand -base64 32)
```

✅ **限制 /internal/config 访问**：
- 仅允许内部网络访问
- 使用防火墙规则限制
- 在 Nginx/Gateway 层面屏蔽外部访问

### 2. 配置管理

✅ **使用版本控制管理 .env.example**：
```bash
# .env.example（提交到 Git）
JWT_SECRET=change-me-in-production
POSTGRES_PASSWORD=change-me

# .env（不提交，添加到 .gitignore）
JWT_SECRET=actual-production-secret
POSTGRES_PASSWORD=actual-password
```

✅ **使用 Secrets 管理工具**（生产环境）：
- Kubernetes Secrets
- HashiCorp Vault
- AWS Secrets Manager
- Azure Key Vault

### 3. 监控与告警

✅ **监控配置加载状态**：

```go
// 添加 Prometheus 指标
configLoadSuccess := prometheus.NewGauge(prometheus.GaugeOpts{
    Name: "config_load_success",
    Help: "Whether config was successfully loaded from System",
})

if err := cfg.loadSharedConfig(systemURL); err != nil {
    configLoadSuccess.Set(0)  // 失败
} else {
    configLoadSuccess.Set(1)  // 成功
}
```

✅ **设置告警规则**：
- 当模块降级到本地配置时发送告警
- 当配置 API 返回错误时发送告警

### 4. 测试

✅ **测试降级机制**：

```bash
# 停止 System
pkill -f "system.*cmd/server/main.go"

# 启动 Manager（应该降级到本地配置）
cd manager/backend && go run cmd/server/main.go
# 应显示: ⚠️  Falling back to local environment variables...
```

✅ **集成测试**：

```go
// 测试配置加载
func TestConfigLoading(t *testing.T) {
    // 启动 mock System server
    mockSystem := httptest.NewServer(...)

    // 配置模块连接到 mock
    os.Setenv("SYSTEM_SERVICE_URL", mockSystem.URL)

    // 加载配置
    cfg := config.Load()

    // 验证配置正确加载
    assert.Equal(t, "mock-jwt-secret", cfg.JWTSecret)
}
```

### 5. 部署

✅ **Docker 部署时确保网络连通**：

```yaml
# docker-compose.yml
services:
  system:
    ...
    networks:
      - addp-network

  manager:
    ...
    environment:
      - SYSTEM_SERVICE_URL=http://system:8080  # 使用服务名
    networks:
      - addp-network
    depends_on:
      - system

networks:
  addp-network:
    driver: bridge
```

✅ **Kubernetes 部署使用 Service Discovery**：

```yaml
# manager-deployment.yaml
env:
  - name: SYSTEM_SERVICE_URL
    value: "http://system-service.default.svc.cluster.local:8080"
```

---

## 总结

配置中心模式带来的好处：

✅ **简化配置管理** - 一处修改，处处生效
✅ **提高安全性** - 敏感配置集中加密管理
✅ **增强灵活性** - 支持集成和独立两种部署模式
✅ **降低维护成本** - 减少配置重复和不一致风险

---

## 附录

### A. 配置项完整清单

#### System 模块 `.env`

```bash
# System 特有
PORT=8080
DATABASE_URL=/app/data/system.db
ENV=production
PROJECT_NAME=全域数据平台

# 共享配置（其他模块从这里获取）
JWT_SECRET=<64-char-secret>
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_USER=addp
POSTGRES_PASSWORD=<password>
POSTGRES_DB=addp
ENCRYPTION_KEY=<base64-32-bytes>

# 可选
INTERNAL_API_KEY=<api-key>
```

#### Manager 模块 `.env`

```bash
PORT=8081
DB_SCHEMA=manager
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true
INTERNAL_API_KEY=<api-key>
```

#### Meta 模块 `.env`

```bash
PORT=8082
DB_SCHEMA=metadata
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true
INTERNAL_API_KEY=<api-key>

# Meta 特有
AUTO_SYNC_ENABLED=true
AUTO_SYNC_SCHEDULE=0 0 * * *
AUTO_SYNC_LEVEL=database
DEEP_SCAN_TIMEOUT=30m
DEEP_SCAN_BATCH_SIZE=10
```

#### Transfer 模块 `.env`

```bash
PORT=8083
DB_SCHEMA=transfer
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true
INTERNAL_API_KEY=<api-key>

# Transfer 特有
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=addp_redis
WORKER_COUNT=5
CONCURRENT_TASKS=10
MAX_RETRIES=3
RETRY_DELAY=30s
TASK_QUEUE_NAME=transfer:tasks
```

### B. API 参考

#### GET /internal/config

**请求**：
```http
GET /internal/config HTTP/1.1
Host: localhost:8080
X-Internal-API-Key: your-api-key  (可选)
```

**响应**：
```json
{
  "jwt_secret": "your-jwt-secret",
  "database": {
    "host": "localhost",
    "port": "5432",
    "user": "addp",
    "password": "addp_password",
    "name": "addp"
  },
  "encryption_key": "ZGV2LWVuY3J5cHRpb24ta2V5LTMyLWJ5dGVzIQ=="
}
```

#### GET /api/resources

详见 System 模块 API 文档。

---

**文档版本**: 1.0
**最后更新**: 2025-10-05
**维护者**: ADDP Team
