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
)

// 使用 JWT token 创建客户端
client := commonClient.NewSystemClient(systemURL, jwtToken)

// 列出所有引擎
engines, err := client.ListEngines("postgresql")

// 获取特定引擎
engine, err := client.GetEngine(engineID)

// 使用 engine.ConnectionInfo 作为连接信息事实源
// 需要底层 driver DSN 的数据库类引擎，由对应 engine plugin 的 DSNProvider.BuildDSN() 构建
connInfo := engine.ConnectionInfo
```

**模块 .env 文件**:

每个模块只需配置模块特定的设置:

```bash
# Manager/Meta/Transfer .env
PORT=8081                          # 模块特定端口
DB_SCHEMA=manager                  # 模块特定 schema
SYSTEM_URL=http://localhost:8180
ENABLE_SERVICE_INTEGRATION=true    # 启用配置中心

# 共享配置 (JWT_SECRET, DB 连接) 从 System 获取
# 回退配置已注释 (仅在集成禁用时使用)
```

### Manager 导入中转对象存储

Manager 的 Shapefile 导入不是由 Manager 自己解析写库。Manager 只负责接收 ZIP 包、上传到中转对象存储，然后创建并触发 Transfer `sync`：

```json
{
  "source": {
    "locator": "addp-infra://minio/manager/tenant_7/import/20260622/upload-uuid/roads.shp?type=object",
    "data_type": "table",
    "representation": "encoded",
    "format": "shapefile"
  }
}
```

Manager 上传导入文件时写入 ADDP infra MinIO 的 `manager` bucket，并通过 `addp-infra://minio/manager/...` locator 调用 Transfer。infra MinIO 不进入 System engines，上传暂存对象也不进入 Meta。
对于 Shapefile 多文件上传，source locator 指向 primary `.shp`；同 basename 的 `.dbf`、`.shx`、`.prj`、`.cpg` 等组件随同写入同一暂存目录，由 Transfer 按格式能力读取相关 refs。

相关环境变量：

```bash
MINIO_SYSTEM_ENDPOINT=localhost:19000
MINIO_SYSTEM_ACCESS_KEY=minioadmin
MINIO_SYSTEM_SECRET_KEY=minioadmin
```

规则：

1. Manager 负责上传暂存和后续 cleanup，Transfer 只按 locator 读取。
2. 暂存路径使用 `tenant_{tenant_id}/import/{yyyymmdd}/{upload_uuid}/...`。
3. 当前导入入口支持一个 Shapefile ZIP 包，或浏览器同时选择同一套 Shapefile 的多个组件文件；`.shp/.dbf/.shx` 必须同 basename，不能混入多套 Shapefile。

### Manager 栅格 mosaic 生成配置

栅格 mosaic 生成是离线任务，Manager 通过 Python Workflow 的 `build_raster_mosaic` 算子执行 GDAL 处理。该调用不同于在线瓦片渲染，允许更长的执行预算：

```bash
# 栅格 mosaic 生成算子调用超时。默认 2 小时。
RASTER_MOSAIC_GENERATION_TIMEOUT=2h

# 容器版 Python Workflow 的 gunicorn worker 超时。默认 7200 秒。
PYTHON_WORKFLOW_GUNICORN_TIMEOUT=7200
```

leaf COG 生成并发不通过全局环境变量固定，而是在任务 `config.cog` 中归一化为明确值。默认策略按运行机器 CPU 预算计算：逻辑 CPU 小于 8 时 `leaf_concurrency=1`，8 到 15 时为 `2`，16 到 31 时为 `4`，32 及以上时为 `6`，上限 `8`；单个 leaf COG 的 GDAL `num_threads` 默认按 `逻辑 CPU / (leaf_concurrency * 2)` 计算并限制在 `1` 到 `4`。当前 18 逻辑 CPU 开发机默认得到 `leaf_concurrency=4`、`num_threads=2`。`cog.leaf_retry_attempts` 默认 `2`，上限 `5`，用于单个 leaf COG 生成或校验的瞬时失败重试。`detached` 模式重跑时会复用目标数据集中已经存在且内容级 COG 校验通过的 leaf，因此超时或中断后的恢复通过再次执行同一任务继续完成未生成部分，而不是从头覆盖全部 leaf。

### Manager 向量化配置

Manager 向量化当前阶段只允许一个启用中的向量模型和一个向量维度。任务定义中的 `config.embedding.model` / `config.embedding.dimension` 是当前配置快照，创建或更新任务时必须与以下环境变量一致；不再按 text/image/video 分别配置模型。

```bash
MANAGER_EMBEDDING_SERVICE_BASE_URL=
MANAGER_EMBEDDING_SERVICE_API_KEY=
MANAGER_EMBEDDING_SERVICE_TIMEOUT=15s
MANAGER_EMBEDDING_MODEL=qwen3-vl-embedding
MANAGER_VECTOR_DIMENSION=2560
MANAGER_VECTOR_SEARCH_MAX_DISTANCE=0.78
MANAGER_VECTOR_MAX_FILE_SIZE_MB=10
MANAGER_VECTOR_BATCH_CONCURRENCY=5
```

### Manager 快显与动态 MVT 配置

Manager 快显中的动态 MVT 是交互式预览能力，单瓦片查询必须受响应时间预算保护。以下配置同时影响能力接口返回的 `realtime_tile.timeout_budget_ms`、动态 MVT 查询的实际超时控制，以及超时响应头中的诊断信息。

```bash
# 小数据量直接 GeoJSON 快显推荐阈值。PG 空间表超过该阈值仍可使用动态 MVT。
QUICK_VIEW_DIRECT_GEOJSON_MAX_ROWS=2000

# 动态 MVT 单瓦片交互超时预算，单位毫秒。
QUICK_VIEW_REALTIME_TILE_TIMEOUT_MS=5000

# 动态 MVT 在 ready 3857 目标路径下仍超时时，前端可按 TTL 重试的建议间隔，单位秒。
QUICK_VIEW_REALTIME_TILE_RETRY_AFTER_SEC=60
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

# Infra MinIO - 基础设施级对象存储
# 用于系统文件、模块缓存、审计日志归档等，不等于业务对象存储引擎。
MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=minioadmin
MINIO_API_PORT=19000
MINIO_BUCKET=system

# MinIO - 业务数据 (部署在 business/docker-compose.yml)
# 注意：Business 引擎连接信息由 ADDP 容器内服务使用，生产 Docker 部署请使用 business 网络服务名，不要写 localhost。
BUSINESS_PG_HOST=business-postgres
BUSINESS_PG_PORT=5432
BUSINESS_PG_USER=business
BUSINESS_PG_PASSWORD=business_password
BUSINESS_PG_DB=business
BUSINESS_MINIO_ENDPOINT=business-minio:9000
BUSINESS_MINIO_ACCESS_KEY=minioadmin
BUSINESS_MINIO_SECRET_KEY=minioadmin

# 服务集成
ENABLE_SERVICE_INTEGRATION=true  # 启用跨服务调用

# 审计日志归档
AUDIT_LOG_RETENTION_DAYS=90
AUDIT_LOG_ARCHIVE_ENABLED=false
AUDIT_LOG_ARCHIVE_CRON="0 2 * * *"
```

### 端口分配

详见 [addp端口分配.md](addp端口分配.md)。

**推荐访问**:
- **生产环境**: http://localhost:80 (通过 Nginx 访问 Console 控制台)
- **开发环境**: http://localhost:5170 (Console 独立访问) 或各模块独立端口

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
curl -X POST http://localhost:8180/api/v1/system/login \
  -H "Content-Type: application/json" \
  -d '{"username": "SuperAdmin", "password": "20251001#SuperAdmin"}'

# 使用租户管理员登录
curl -X POST http://localhost:8180/api/v1/system/login \
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

- `POST /api/v1/system/login` - 登录
- `POST /api/v1/system/register` - 注册

**受保护** (需要 JWT):

- `GET /api/v1/system/users/me` - 当前用户
- `GET /api/v1/system/users` - 列出用户
- `GET/PUT/DELETE /api/v1/system/users/:id` - 用户 CRUD
- `GET /api/v1/system/logs` - 审计日志 (支持 `?user_id=X` 过滤)
- `POST/GET/PUT/DELETE /api/v1/system/engines` - 引擎 CRUD (支持 `?engine_type=X` 过滤)

**另请参阅**: 本文即为当前配置中心与环境变量说明入口。
