### 配置中心模式
**System 作为单一真实来源**:

平台实现了集中式配置管理模式,其中 **System 模块充当所有其他模块的配置中心**。

**集中化的内容**:

1. **授权上下文**: opaque 用户 Token 由 System `/api/v1/system/auth/context` 解析，业务模块不解析 Token
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
   │  └─ 使用 System 配置 (DB 连接、加密和内部调用配置)
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

// 使用当前请求的短期 User Access Token 创建客户端
client := commonClient.NewSystemClient(systemURL, userAccessToken)

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

# 共享配置 (DB 连接、加密和内部调用配置) 从 System 获取
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

栅格 mosaic 生成是离线任务，Manager 通过 GeoPython Workflow 的 `build_raster_mosaic` 算子执行 GDAL 处理。该调用不同于在线瓦片渲染，允许更长的执行预算：

```bash
# 栅格 mosaic 生成算子调用超时。默认 2 小时。
RASTER_MOSAIC_GENERATION_TIMEOUT=2h

# 容器版 GeoPython Workflow 的 gunicorn worker 超时。默认 7200 秒。
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

### Transfer continuous 运行观测配置

Transfer continuous worker 自己采集业务 Kafka或内部 CDC topic 的分区 earliest/latest position，并把 lag 和 retention 恢复窗口写入统一 execution metadata。Monitor 只读取该 metadata，不直连 Kafka。PostgreSQL CDC consumer 使用 Infra Kafka 独立 `transfer` principal，读取 `INFRA_KAFKA_BOOTSTRAP_SERVERS`、`INFRA_KAFKA_TRANSFER_PASSWORD`、`INFRA_KAFKA_SECURITY_PROTOCOL` 和相同 TLS 配置；这些部署字段不进入 System Engine 或任务 JSON。

```bash
# 分区位置与 retention 观测采样间隔，默认 15 秒。
TRANSFER_CONTINUOUS_DIAGNOSTICS_INTERVAL=15s

# 估算剩余恢复时间不大于该值时进入 degraded，默认 6 小时。
TRANSFER_CONTINUOUS_RETENTION_DEGRADED_HORIZON=6h

# 估算剩余恢复时间不大于该值时进入 critical，默认 1 小时。
TRANSFER_CONTINUOUS_RETENTION_CRITICAL_HORIZON=1h

# 存在 source lag 且真实 position commit 超过该时长未推进时，checkpoint health 进入 degraded，默认 5 分钟。
TRANSFER_CONTINUOUS_CHECKPOINT_STALE_AFTER=5m

# 自动恢复首次退避、最大退避、最大连续失败次数、circuit 冷却时间和稳定运行阈值。
TRANSFER_CONTINUOUS_RECOVERY_INITIAL_BACKOFF=1s
TRANSFER_CONTINUOUS_RECOVERY_MAX_BACKOFF=1m
TRANSFER_CONTINUOUS_RECOVERY_MAX_CONSECUTIVE_FAILURES=5
TRANSFER_CONTINUOUS_RECOVERY_CIRCUIT_OPEN_DURATION=5m
TRANSFER_CONTINUOUS_RECOVERY_STABILITY_WINDOW=5m
```

critical 阈值必须小于 degraded 阈值；checkpoint 停滞阈值必须大于 diagnostics 采样间隔。恢复初始退避不得大于最大退避，最大连续失败次数必须为正，circuit 冷却时间和稳定运行阈值必须为正。这些阈值是 Transfer runtime 统一运维策略，不写入用户 task config，也不按任务开放第二条判定路径。

Monitor 每隔以下时间评估最新 active execution 的公共 metadata，将观测信号物化为告警事件。该配置属于 Monitor 部署策略，不进入任务 JSON；Monitor 不因此读取 Transfer 私有表或业务 Kafka。

```bash
MONITOR_ALERT_EVALUATION_INTERVAL=15s

# Webhook delivery dispatcher 轮询周期、HTTP 超时和 claim lease。
MONITOR_WEBHOOK_DISPATCH_INTERVAL=2s
MONITOR_WEBHOOK_HTTP_TIMEOUT=10s
MONITOR_WEBHOOK_LEASE_DURATION=30s

# delivery 最大尝试次数和指数退避边界。
MONITOR_WEBHOOK_MAX_ATTEMPTS=8
MONITOR_WEBHOOK_RETRY_INITIAL_BACKOFF=5s
MONITOR_WEBHOOK_RETRY_MAX_BACKOFF=5m

# 默认 false：拒绝 HTTP、环回、私网、链路本地和云 metadata 目标。
# 只在本地联调或明确受管内网部署中开启。
MONITOR_WEBHOOK_ALLOW_PRIVATE_NETWORKS=false

# Webhook payload 内告警详情链接使用的 Console 外部地址。
MONITOR_CONSOLE_BASE_URL=http://localhost:5170

# 邮件 outbox dispatcher 的轮询、SMTP 超时、领取租约和重试策略。
MONITOR_EMAIL_DISPATCH_INTERVAL=2s
MONITOR_EMAIL_SMTP_TIMEOUT=15s
MONITOR_EMAIL_LEASE_DURATION=30s
MONITOR_EMAIL_MAX_ATTEMPTS=8
MONITOR_EMAIL_RETRY_INITIAL_BACKOFF=5s
MONITOR_EMAIL_RETRY_MAX_BACKOFF=5m

# 平台统一 SMTP Relay。host 为空时邮件 dispatcher 不启动。
MONITOR_EMAIL_SMTP_HOST=
MONITOR_EMAIL_SMTP_PORT=587
MONITOR_EMAIL_SMTP_USERNAME=
MONITOR_EMAIL_SMTP_PASSWORD=
MONITOR_EMAIL_SMTP_TLS_MODE=starttls
MONITOR_EMAIL_FROM_ADDRESS=
MONITOR_EMAIL_FROM_NAME=ADDP Monitor
```

Webhook destination 的 HMAC secret 使用平台统一 `ENCRYPTION_KEY` 做 AES-256-GCM 加密，不新增 Monitor 私有加密密钥。dispatcher 配置属于部署策略，不进入任务定义或普通用户请求；`MONITOR_WEBHOOK_ALLOW_PRIVATE_NETWORKS` 只能由部署者设置，API 不提供绕过开关。

邮件第一版只允许 `MONITOR_EMAIL_SMTP_TLS_MODE=starttls|tls`，不支持明文或机会式降级，也不根据端口猜测 TLS 模式。配置 username 时必须同时配置 password，反之亦然；配置 host 时必须配置合法的 from address。SMTP host 为空时 Monitor 仍可管理邮件目标，但不会启动邮件 dispatcher，既有 `pending` outbox 会在部署者补齐配置并重启后继续投递。SMTP 凭据只存在部署环境，不进入 System Engine、任务 JSON、租户 API 或投递审计。

### Infra Kafka、Kafka Connect 与 Capture Supervisor 配置（工作包 3B/3C 已实现）

Infra Kafka 是 ADDP 部署资源，不进入 System Engine。第一版固定 Apache Kafka 4.3.0 KRaft + Debezium Connect 3.6.0.Final；开发环境为 1 broker/1 Connect，生产参考为 3 broker/至少 2 Connect worker。

```bash
# 仅供 ADDP 内部组件使用；不写入用户任务 JSON。
INFRA_KAFKA_BOOTSTRAP_SERVERS=localhost:19092
INFRA_KAFKA_ADMIN_USERNAME=admin
INFRA_KAFKA_ADMIN_PASSWORD=change-in-production
INFRA_KAFKA_CDC_TOPIC_PREFIX=__addp_cdc.
INFRA_KAFKA_CDC_RETENTION=168h
INFRA_KAFKA_CDC_RETENTION_BYTES=
INFRA_KAFKA_CDC_REPLICATION_FACTOR=1
INFRA_KAFKA_SECURITY_PROTOCOL=sasl_plaintext
INFRA_KAFKA_TLS_CA_CERT_FILE=
INFRA_KAFKA_TLS_INSECURE_SKIP_VERIFY=false
INFRA_KAFKA_DISK_DEGRADED_PERCENT=75
INFRA_KAFKA_DISK_CRITICAL_PERCENT=85

KAFKA_CONNECT_URL=http://localhost:18083
KAFKA_CONNECT_USERNAME=
KAFKA_CONNECT_PASSWORD=
KAFKA_CONNECT_TIMEOUT=15s
KAFKA_CONNECT_LOOPBACK_HOST=host.docker.internal
KAFKA_CONNECT_GROUP_ID=addp-connect-cluster
KAFKA_CONNECT_CONFIG_TOPIC=__addp_connect_configs
KAFKA_CONNECT_OFFSET_TOPIC=__addp_connect_offsets
KAFKA_CONNECT_STATUS_TOPIC=__addp_connect_status

TRANSFER_CAPTURE_PROVISIONING_TIMEOUT=60s
TRANSFER_CAPTURE_STATUS_POLL_INTERVAL=1s
TRANSFER_CAPTURE_MONITOR_INTERVAL=15s
TRANSFER_CAPTURE_RUNTIME_STOP_TIMEOUT=45s
TRANSFER_CAPTURE_RUNTIME_STOP_POLL_INTERVAL=250ms
```

生产环境必须显式设置 `INFRA_KAFKA_CDC_RETENTION_BYTES`，并按峰值写入速率、7 天恢复窗口、副本因子和安全余量校验磁盘容量；time/bytes 任一边界先到都会删除旧 segment。开发状态脚本按 75%/85% 展示 degraded/critical 磁盘水位。凭据分别由 infra admin、Kafka Connect 和 Transfer consumer 使用，不复用业务 Kafka Engine 凭据。单机开发 Compose 使用仅限本机和 Docker 网络的 `SASL_PLAINTEXT/PLAIN`；生产必须切换到 `SASL_SSL` 或等价 TLS，并把 Connect REST 保持在内部网络。`KAFKA_CONNECT_LOOPBACK_HOST` 只用于 Connect 容器访问登记为 localhost/loopback 的开发业务库，不改写远程数据库主机。capture supervisor 已负责显式创建单分区 CDC topic、托管 connector、登记 generation/resource、监控状态和幂等 stop/cleanup；PostgreSQL CDC 产品入口仍需等待 3D 数据面闭环，不能仅凭控制面存在就开放 capability。

### Manager 快显与动态 MVT 配置

Manager 快显中的动态 MVT 是交互式预览能力，单瓦片查询必须受响应时间预算保护。以下配置同时影响能力接口返回的 `realtime_tile.timeout_budget_ms`、动态 MVT 查询的实际超时控制，以及超时响应头中的诊断信息。

```bash
# 中小数据量直接 FlatGeobuf 快显推荐阈值。PG 空间表超过该阈值仍可使用动态 MVT。
QUICK_VIEW_DIRECT_FLATGEOBUF_MAX_ROWS=2000

# 动态 MVT 单瓦片交互超时预算，单位毫秒。
QUICK_VIEW_REALTIME_TILE_TIMEOUT_MS=5000

# 动态 MVT 在 ready 3857 目标路径下仍超时时，前端可按 TTL 重试的建议间隔，单位秒。
QUICK_VIEW_REALTIME_TILE_RETRY_AFTER_SEC=60
```

## 配置

### 环境变量

根目录 `.env` 文件 (从 `.env.example` 复制):

```bash
# System OAuth 用户令牌生命周期
ACCESS_TOKEN_EXPIRE_MINUTES=15
DELEGATED_ACCESS_TOKEN_EXPIRE_MINUTES=2
RESOURCE_ACCESS_TICKET_EXPIRE_MINUTES=15
REFRESH_TOKEN_EXPIRE_DAYS=30
OAUTH_CODE_EXPIRE_MINUTES=5
OAUTH_DEVICE_EXPIRE_MINUTES=10
OAUTH_DEVICE_INTERVAL_SECONDS=5
OAUTH_PUBLIC_RATE_LIMIT_PER_MINUTE=60
OAUTH_USER_RATE_LIMIT_PER_MINUTE=30
CONSOLE_URL=http://localhost:5170

# System 只信任这些反向代理提供的客户端 IP 转发头；逗号分隔 IP 或 CIDR。
# 容器/生产环境必须显式加入实际 Gateway/Nginx 网段，禁止配置 0.0.0.0/0 或 ::/0。
TRUSTED_PROXIES=127.0.0.1,::1

# 开发环境各前端通过 credentials 调用 System 登录和静默刷新。
# 必须覆盖 Console 和所有独立模块前端端口。
ALLOWED_ORIGINS=http://localhost:5170,http://localhost:5173,http://localhost:5174,http://localhost:5175,http://localhost:5176,http://localhost:5177,http://localhost:5178,http://localhost:5179,http://localhost:5180,http://localhost:5181,http://localhost:5182,http://localhost:5183,http://localhost:5184,http://localhost:5185,http://localhost:5186,http://localhost:5187

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
# 启用默认租户和开发用本地管理员账户创建
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
# 使用开发用本地管理员账户登录
curl -X POST http://localhost:8180/api/v1/system/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "123456"}'
```

目标 IAM 不创建常驻 `SuperAdmin` 默认账号。平台系统管理员、安全管理员和审计管理员必须通过受控初始化与角色任命流程建立，不能通过环境变量写入共享默认密码。

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

**受保护**（需要 User Access Token）:

- `GET /api/v1/system/users/me` - 当前用户
- `GET /api/v1/system/users` - 列出用户
- `GET/PUT/DELETE /api/v1/system/users/:id` - 用户 CRUD
- `GET /api/v1/system/logs` - 审计日志 (支持 `?user_id=X` 过滤)
- `POST/GET/PUT/DELETE /api/v1/system/engines` - 引擎 CRUD (支持 `?engine_type=X` 过滤)

**另请参阅**: 本文即为当前配置中心与环境变量说明入口。
