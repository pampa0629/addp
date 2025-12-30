### 配置中心模式
**System 作为单一真实来源**:

平台实现了集中式配置管理模式,其中 **System 模块充当所有其他模块的配置中心**。

**集中化的内容**:

1. **认证**: `JWT_SECRET` - 确保所有服务使用相同的 JWT 签名密钥
2. **系统数据库**: PostgreSQL 连接信息 - 系统数据的单一来源
3. **业务引擎**: System 的 `engines` 表中管理的引擎 - 所有数据源配置
4. **加密密钥**: `ENCRYPTION_KEY` - 跨服务的一致加密

**配置加载流程**:

```
模块启动
   ↓
尝试从 System 获取配置 (/internal/config)
   ↓
   ├─ 成功 ✅
   │  └─ 使用 System 配置 (JWT_SECRET, DB 连接)
   │
   └─ 失败 ⚠️
      └─ 回退到本地 .env 配置
```

**优势**:

- ✅ **单一真实来源**: 修改数据库密码一次,重启服务即可应用
- ✅ **安全性**: 敏感配置集中管理和加密
- ✅ **灵活性**: 支持集成和独立部署模式
- ✅ **可维护性**: 减少配置重复,更易于审计

**SystemClient 使用**:

所有模块使用 `SystemClient` 从 System 获取业务数据库配置:

```go
import (
    commonClient "github.com/addp/common/client"
    commonModels "github.com/addp/common/models"
)

// 使用 JWT token 创建客户端
client := commonClient.NewSystemClient(systemURL, jwtToken)

// 列出所有引擎
engines, err := client.ListEngines("postgresql")

// 获取特定引擎
engine, err := client.GetEngine(engineID)

// 构建连接字符串 (密码自动解密)
connStr, err := commonModels.BuildConnectionString(engine)
```

**模块 .env 文件**:

每个模块只需配置模块特定的设置:

```bash
# Manager/Meta/Transfer .env
PORT=8081                          # 模块特定端口
DB_SCHEMA=manager                  # 模块特定 schema
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true    # 启用配置中心

# 共享配置 (JWT_SECRET, DB 连接) 从 System 获取
# 回退配置已注释 (仅在集成禁用时使用)
```

## 配置

### 环境变量

根目录 `.env` 文件 (从 `.env.example` 复制):

```bash
# 安全性 (生产环境必须更改)
JWT_SECRET=your-super-secret-jwt-key-change-this-in-production

# PostgreSQL - ADDP 系统数据库
POSTGRES_PASSWORD=addp_password
POSTGRES_USER=addp
POSTGRES_DB=addp

# Redis
REDIS_PASSWORD=addp_redis

# MinIO - 系统文件
MINIO_SYSTEM_ROOT_USER=minioadmin
MINIO_SYSTEM_ROOT_PASSWORD=minioadmin

# MinIO - 业务数据 (部署在 business/docker-compose.yml)
BUSINESS_MINIO_ENDPOINT=host.docker.internal:9002
BUSINESS_MINIO_ACCESS_KEY=minioadmin
BUSINESS_MINIO_SECRET_KEY=minioadmin

# 服务集成
ENABLE_SERVICE_INTEGRATION=true  # 启用跨服务调用
```

### 端口分配

**ADDP 系统服务**:

| 服务              | 开发端口 | Docker 端口 | 说明                   |
| -------------------- | -------- | ----------- | ----------------------------- |
| **Nginx Gateway**    | **80**   | **80**      | **统一入口 (推荐)** |
| **Portal Frontend**  | **5170** | **5170**    | **Portal UI (通过 Nginx)**     |
| Gateway              | 8000     | 8000        | API Gateway (后端路由) |
| System Backend       | 8080     | 8080        | 认证、用户、日志             |
| System Frontend      | 5173     | 8090        | 独立访问             |
| Manager Backend      | 8081     | 8081        | 数据源、文件           |
| Manager Frontend     | 5174     | 8091        | 独立访问             |
| Meta Backend         | 8082     | 8082        | 元数据、血缘             |
| Meta Frontend        | 5175     | 8092        | 独立访问             |
| Transfer Backend     | 8083     | 8083        | 导入/导出任务           |
| Transfer Frontend    | 5176     | 8093        | 独立访问             |
| Orchestrator Backend | 8084     | 8084        | 工作流编排        |
| Orchestrator Frontend| 5177     | 8094        | 独立访问             |
| Develop Backend      | 8085     | 8085        | 开发工具             |
| Develop Frontend     | 5178     | 8095        | 独立访问             |
| Python Workflow Engine     | 8099     | 8099        | 空间计算引擎 (Python) |
| PostgreSQL (System)  | 5432     | 5432        | ADDP 系统元数据          |
| Redis                | 6379     | 6379        | 缓存和队列                 |
| MinIO System API     | 9000     | 9000        | 系统文件存储           |
| MinIO System Console | 9001     | 9001        | 系统 MinIO Web UI           |
| Meilisearch          | 7700     | 7700        | 全文检索引擎       |

**业务库服务** (通过 `business/docker-compose.yml` 部署):

| 服务                | Docker 端口 | 说明                |
| ---------------------- | ----------- | -------------------------- |
| PostgreSQL (Business)  | 5433        | 用户业务数据存储 |
| MinIO Business API     | 9002        | 用户文件存储          |
| MinIO Business Console | 9003        | 业务 MinIO Web UI      |

**推荐访问**:
- **生产环境**: http://localhost:80 (通过 Nginx 访问 Portal 统一入口)
- **开发环境**: http://localhost:5170 (Portal 独立访问) 或各模块独立端口

**业务库设置**:

```bash
cd business
cp .env.example .env
docker-compose up -d
```

#### 启用默认租户账户

在 `.env` 文件中添加以下配置:

```bash
# 启用默认租户和租户管理员账户创建
ENABLE_DEFAULT_TENANT=true

# 可选: 自定义默认账户信息
DEFAULT_TENANT_NAME=默认租户
DEFAULT_ADMIN_USERNAME=admin
DEFAULT_ADMIN_PASSWORD=123456
DEFAULT_ADMIN_EMAIL=admin@addp.com
```

#### 安全提示

- ⚠️ **仅用于开发和测试环境** - 这些账户密码较弱,不应在生产环境使用
- ⚠️ **生产环境强制禁用** - 即使设置 `ENABLE_DEFAULT_TENANT=true`,在 `ENV=production` 时也不会创建
- ⚠️ **默认禁用** - 未设置 `ENABLE_DEFAULT_TENANT=true` 时不会创建默认租户账户
- 💡 可通过环境变量自定义账户信息 (用户名、密码、邮箱等)
- 💡 账户创建是幂等的,重复启动不会重复创建

#### 登录测试

使用默认账户登录:

```bash
# 使用超级管理员登录
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "SuperAdmin", "password": "20251001#SuperAdmin"}'

# 使用租户管理员登录
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "123456"}'
```

**初始化位置**: `system/backend/internal/repository/database.go`


**数据持久化**:

**ADDP 系统** (docker-compose.infra.yml):

- PostgreSQL: `postgres_data` 卷 (ADDP 系统元数据)
- Redis: `redis_data` 卷 (缓存和队列)
- MinIO System: `minio_data` 卷 (系统文件)
- Meilisearch: `meilisearch_data` 卷 (搜索索引)

**业务库** (business/docker-compose.yml):

- PostgreSQL: `business_postgres_data` 卷 (用户业务数据)
- MinIO Business: `business_minio_data` 卷 (用户文件)

## API 端点摘要

**公开**:

- `POST /api/auth/login` - 登录
- `POST /api/auth/register` - 注册

**受保护** (需要 JWT):

- `GET /api/users/me` - 当前用户
- `GET /api/users` - 列出用户
- `GET/PUT/DELETE /api/users/:id` - 用户 CRUD
- `GET /api/logs` - 审计日志 (支持 `?user_id=X` 过滤)
- `POST/GET/PUT/DELETE /api/engines` - 引擎 CRUD (支持 `?engine_type=X` 过滤)

**另请参阅**: `docs/CONFIG_CENTER.md` 获取详细的配置中心使用指南。