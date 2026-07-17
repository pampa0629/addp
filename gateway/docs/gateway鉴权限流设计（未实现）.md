# Gateway 鉴权限流设计（未实现）

## 目录

1. [当前实现状态](#当前实现状态)
2. [Service 数据服务鉴权限流设计](#service-数据服务鉴权限流设计)
3. [限流配置管理增强](#限流配置管理增强)
4. [服务注册与发现改造计划](#服务注册与发现改造计划)
5. [未来扩展功能](#未来扩展功能)

---

## 当前实现状态

### 已实现功能

**1. Gateway 对 ADDP 内置模块的鉴权限流**

- ✅ **API Key 认证**
  - 三层缓存验证（本地缓存 5分钟 → Redis 1小时 → System API）
  - SHA256 哈希存储，安全可靠
  - 支持服务级别访问控制（allowed_services）

- ✅ **限流控制**
  - 基于 Application 的限流（rate_limit_per_minute）
  - Redis 令牌桶算法，分布式一致性
  - 超限返回 429 Too Many Requests

- ✅ **配置管理**
  - 在 System 模块的 Application 管理界面设置
  - 可配置每个 Application 的请求速率限制
  - 支持 API Key 撤销和缓存失效

### 未实现功能

**1. Service 模块对外发布的数据服务鉴权限流**
- ❌ Service 模块发布的 OGC 服务（WFS/WMTS/OGC API Features）的鉴权
- ❌ 数据查询服务的细粒度限流
- ❌ 按服务、按图层、按租户的独立限流配置
- ❌ 公开服务（public_access=true）的访问控制策略

**2. 限流配置管理界面增强**
- ❌ 实时监控限流状态的界面
- ❌ 动态调整限流阈值的界面
- ❌ 限流统计报表和趋势分析

---

## Service 数据服务鉴权限流设计

### 需求背景

Service 模块是 ADDP 平台的**统一数据服务门户**，负责将平台管理的数据发布为标准化服务：

- **内部服务**：将 ADDP 管理的数据（数据库表、对象存储文件）发布为 OGC 标准服务（WFS、WMTS、OGC API Features）
- **外部服务**：注册和代理第三方数据服务（天地图、高德地图等）

这些服务需要完善的鉴权和限流机制：
- **鉴权**：验证用户身份，控制访问权限（公开 vs 私有服务）
- **限流**：防止恶意攻击，保护后端资源，确保服务稳定性

### 设计方案

#### 方案 1：Gateway 统一鉴权（推荐）

**架构设计**

```
客户端请求
  ↓
Gateway (8000)
  ├─ API Key 验证 / User Access Token + AuthContext
  ├─ 限流控制（Application 级 + 服务级）
  └─ 转发到 Service 模块 (8086)
     ↓
Service 模块
  ├─ 验证服务访问权限（tenant_id、public_access）
  ├─ 查询数据源（System、Meta 模块）
  └─ 返回服务响应（GeoJSON、MVT、XML）
```

**核心特性**

1. **复用现有鉴权机制**
   - Service 发布的服务通过 Gateway 统一路由（`/api/service/*`、`/ogc/*`）
   - 复用 Gateway 的 API Key 转发和 User Access Token 主路径
   - 统一的认证策略和日志审计

2. **两层限流结合**
   - **Application 级限流**（已实现）：每个 API Key 的全局请求速率限制
   - **服务级限流**（待实现）：每个服务/图层的独立限流配置

3. **公开服务支持**
   - Gateway 识别公开服务路径（如 `/ogc/wfs/public_*`）
   - 公开服务不强制 User Access Token，但仍受限流保护
   - Service 模块验证服务的 `public_access=true` 标志

**实施步骤**

```
步骤 1：Service 服务注册到 Gateway
  - Service 模块启动时，向 System 模块注册服务元数据
  - 包含：服务名称、服务类型（内部/外部）、访问控制（public/private）
  - Gateway 从 System 模块读取服务列表，动态生成路由规则

步骤 2：Gateway 路由规则扩展
  - 新增路由：/ogc/wfs/{service_name}、/ogc/wmts/{service_name}、/ogc/api/{service_name}
  - 公开服务：不强制 API Key/User Access Token 认证
  - 私有服务：需要 API Key 或 User Access Token

步骤 3：服务级限流配置
  - System 模块的 internal_services 表增加字段：rate_limit_per_minute、rate_limit_per_hour
  - Gateway 读取服务限流配置，使用 Redis 令牌桶算法
  - 限流 Key：ratelimit:service:{service_id}

步骤 4：Service 模块授权验证
  - Gateway 转发请求时，附加用户信息（user_id、tenant_id）
  - Service 模块验证：
    - 服务是否属于用户租户（tenant_id 匹配）
    - 服务是否公开（public_access=true）
    - 图层级访问控制（可选）
```

**优势**

- ✅ 集中式管理：Gateway 统一处理鉴权和限流，降低 Service 模块复杂度
- ✅ 性能优化：复用三层缓存机制，极高性能
- ✅ 统一审计：所有服务访问日志记录在 Gateway
- ✅ 灵活配置：支持全局限流 + 服务级限流

**劣势**

- ⚠️ 耦合度：Service 模块无法完全独立部署
- ⚠️ Gateway 复杂度增加：需要处理更多路由规则和配置

---

#### 方案 2：Service 模块独立鉴权

**架构设计**

```
客户端请求
  ↓
Service 模块 (8086)
  ├─ JWT 验证 / API Key 验证（独立实现）
  ├─ 限流控制（独立实现）
  ├─ 服务访问权限验证
  └─ 返回服务响应
```

**核心特性**

1. **独立鉴权实现**
   - Service 模块通过 System AuthContext 验证 User Access Token，或按应用身份验证 API Key
   - 从 System 模块获取用户信息和权限
   - 独立的限流逻辑（基于 Redis）

2. **服务级细粒度控制**
   - 每个服务、每个图层独立的访问控制策略
   - 支持复杂的权限模型（如：某些图层仅对特定用户开放）

**优势**

- ✅ 独立部署：Service 模块可以独立部署和测试
- ✅ 灵活性：支持服务级细粒度权限控制

**劣势**

- ❌ 重复逻辑：与 Gateway 鉴权限流逻辑重复，违反 DRY 原则
- ❌ 维护成本高：需要在 Service 模块维护独立的鉴权限流代码
- ❌ 配置分散：鉴权配置分散在 Gateway 和 Service 模块

---

### 推荐方案：方案 1（Gateway 统一鉴权）

**决策理由**

1. **DRY 原则**：复用 Gateway 已有的鉴权限流机制，避免重复代码
2. **统一管理**：Gateway 作为平台唯一入口，统一处理鉴权和限流
3. **性能优越**：三层缓存机制已经过验证，性能极高
4. **ADDP 架构**：符合平台微服务架构设计，Gateway 承担统一网关职责

**实施优先级**：P1（高优先级）

---

## 限流配置管理增强

### 当前限流配置

**配置位置**：System 模块的 `applications` 表

| 字段 | 类型 | 说明 | 示例值 |
|------|------|------|--------|
| rate_limit_per_minute | int | 每分钟请求数限制 | 1000 |

**配置方式**：通过 System 模块的 Application 管理界面设置

**限制**：
- ❌ 仅支持 Application 级限流，无法针对特定服务或模块限流
- ❌ 无法实时查看当前限流状态
- ❌ 无法动态调整限流阈值（需重启服务）

### 待增强功能

#### 1. 实时监控界面

**需求**：可视化展示当前限流状态

**功能点**：
- 显示每个 Application 的当前请求数和限流阈值
- 显示被限流的请求统计（今天、本周、本月）
- 显示限流历史趋势图
- 支持按 Application、服务类型、时间范围筛选

**技术方案**：
- 从 Redis 读取实时限流计数（`ratelimit:app:{app_id}`）
- 从 PostgreSQL 读取历史限流日志（`gateway.api_access_logs` 表）
- 前端使用 Chart.js 或 ECharts 展示趋势图

**实施优先级**：P2

---

#### 2. 动态调整限流阈值

**需求**：无需重启服务即可调整限流配置

**功能点**：
- 在线修改 Application 的 rate_limit_per_minute
- 修改后立即生效（通过 Redis Pub/Sub 通知 Gateway）
- 支持临时调整限流（应对突发流量）

**技术方案**：
```
用户修改限流配置（System 前端）
  ↓
System 模块更新数据库
  ↓
System 模块发布 Redis Pub/Sub 消息
  ↓
Gateway 订阅消息，刷新本地缓存
  ↓
新的限流配置生效
```

**实施优先级**：P2

---

#### 3. 细粒度限流

**需求**：支持多种维度的限流策略

**限流维度**：

| 维度 | 说明 | Key 格式 | 示例 |
|------|------|---------|------|
| Application 级 | 每个 API Key 的全局限流 | `ratelimit:app:{app_id}` | ✅ 已实现 |
| 服务类型级 | 按模块限流 | `ratelimit:service:{service_name}` | ❌ 待实现 |
| 租户级 | 按租户限流 | `ratelimit:tenant:{tenant_id}` | ❌ 待实现 |
| 用户级 | 按用户限流 | `ratelimit:user:{user_id}` | ❌ 待实现 |
| IP 级 | 按 IP 限流（防 DDoS） | `ratelimit:ip:{ip_address}` | ❌ 待实现 |

**配置示例**：
```json
{
  "app_level": {
    "rate_limit_per_minute": 1000
  },
  "service_level": {
    "system": 500,
    "manager": 300,
    "service": 200
  },
  "tenant_level": {
    "rate_limit_per_hour": 10000
  },
  "ip_level": {
    "rate_limit_per_minute": 100
  }
}
```

**实施优先级**：P3

---

## 服务注册与发现改造计划

### 当前架构的局限性

目前 Gateway 采用 **静态配置 + 硬编码路由** 的方式：

```
.env (硬编码服务URL)
  ↓
Gateway 读取配置
  ↓
硬编码路由规则 (router.go:93-142)
  ↓
转发请求到指定模块
```

**主要问题**：

| 问题 | 影响 | 严重程度 |
|------|------|---------|
| 硬编码路由规则 | 每增加一个模块都要修改 Gateway 代码，违反开闭原则 | ⚠️ 中 |
| 静态配置，无动态感知 | 模块宕机或扩容，Gateway 无法感知 | 🔴 高 |
| 无服务健康检查 | 可能将请求转发到已宕机的服务 | 🔴 高 |
| 无负载均衡 | 单个模块多实例时，无法自动分配流量 | ⚠️ 中 |

### 改造方案（分阶段实施）

#### 阶段 1：健康检查（P0 - 立即实施）

**目标**：避免将请求转发到宕机的服务

**方案**：
```
Gateway → /health (每 10s 检查)
    ↓
  ✅ 可用 → 路由
  ❌ 宕机 → 503 错误
```

**实施细节**：
- Gateway 启动时，对所有配置的服务 URL 发起健康检查
- 定时任务每 10 秒检查一次（goroutine + ticker）
- 健康状态存储在内存中（map[string]bool）
- 路由时检查健康状态，宕机服务返回 503 Service Unavailable

**代码量**：约 50 行

**收益**：避免路由到宕机服务，提高用户体验

**实施优先级**：P0（立即实施）

---

#### 阶段 2：PostgreSQL 服务注册（P1 - 推荐）

**目标**：动态服务注册，解耦 Gateway 代码

**架构设计**：

```
┌─────────────┐      启动时注册       ┌─────────────┐
│  模块启动   │ ──────────────────→  │ System DB   │
│ (System等)  │  INSERT service_registry│(services表) │
└─────────────┘                       └─────────────┘
                                            ↓
                                      Gateway 定期拉取
                                            ↓
                                     ┌──────────────┐
                                     │ 动态路由表   │
                                     │ system → :8180│
                                     │ manager→ :8081│
                                     └──────────────┘
```

**核心特性**：
- ✅ 服务启动时自动注册到 System DB
- ✅ 定期心跳上报服务状态（last_heartbeat）
- ✅ Gateway 动态加载路由表（30s 刷新）
- ✅ 自动剔除离线服务（心跳超时）
- ✅ 支持多实例负载均衡

**数据表设计**：

```sql
CREATE TABLE system.service_registry (
    id SERIAL PRIMARY KEY,
    service_name VARCHAR(50) UNIQUE NOT NULL,  -- 'system', 'manager'
    service_url VARCHAR(255) NOT NULL,         -- 'http://localhost:8180'
    service_type VARCHAR(20) DEFAULT 'http',   -- 'http', 'grpc'
    status VARCHAR(20) DEFAULT 'up',           -- 'up', 'down'
    last_heartbeat TIMESTAMP DEFAULT NOW(),
    metadata JSONB,                            -- 版本、权重、负载等
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_service_name ON system.service_registry(service_name);
CREATE INDEX idx_status ON system.service_registry(status);
CREATE INDEX idx_last_heartbeat ON system.service_registry(last_heartbeat);
```

**为什么选择 PostgreSQL？**

- ✅ ADDP 是数据平台，DB 操作更自然
- ✅ 无需新增外部组件（Consul/Etcd）
- ✅ 天然持久化，容错能力强
- ✅ System 模块已有 PostgreSQL 连接，无额外成本
- ✅ 支持事务，数据一致性有保障

**服务注册流程**：

```
模块启动
  ↓
调用 System API: POST /internal/service-registry/register
  - service_name: "manager"
  - service_url: "http://localhost:8081"
  - metadata: {"version": "0.0.24"}
  ↓
System 模块写入 service_registry 表
  ↓
模块启动定时任务：每 30 秒发送心跳
  - POST /internal/service-registry/heartbeat
  ↓
System 模块更新 last_heartbeat 字段
```

**Gateway 路由刷新流程**：

```
Gateway 启动
  ↓
从 service_registry 表加载所有服务
  ↓
构建动态路由表
  ↓
启动定时任务（30 秒刷新）
  ↓
重新加载 service_registry 表
  ↓
对比新旧路由表
  ├─ 新增服务 → 添加路由
  ├─ 删除服务 → 移除路由
  └─ 更新 URL → 修改路由
```

**代码量**：约 200 行

**收益**：
- 动态路由，无需修改 Gateway 代码
- 解耦模块和 Gateway
- 支持模块扩容（多实例）
- 自动剔除宕机服务

**实施优先级**：P1（强烈推荐）

---

#### 阶段 3：完整服务治理（P2 - 长期规划）

**增加特性**：

**1. 负载均衡（多实例轮询、加权）**
- 支持同一服务的多个实例（如：manager-1、manager-2）
- 负载均衡算法：轮询、加权轮询、最少连接
- 配置示例：
  ```json
  {
    "service_name": "manager",
    "instances": [
      {"url": "http://localhost:8081", "weight": 2},
      {"url": "http://localhost:8082", "weight": 1}
    ]
  }
  ```

**2. 熔断降级（Hystrix 模式）**
- 检测服务故障率（如：1分钟内失败率 > 50%）
- 自动熔断：短时间内不再转发请求到故障服务
- 降级策略：返回默认响应或缓存数据

**3. 服务版本管理（灰度发布）**
- 支持同一服务的多个版本（v1、v2）
- 按流量百分比分配（如：90% 流量到 v1，10% 到 v2）
- 支持按用户/租户灰度

**4. 流量统计和分析**
- 实时监控服务 QPS、响应时间、错误率
- 服务依赖拓扑图
- 慢查询分析

**实施时机**：服务数量 > 15 个或需要灰度发布

**实施优先级**：P2（长期规划）

---

### 方案对比

| 方案 | 复杂度 | 适用阶段 | 扩容支持 | 推荐度 |
|------|--------|---------|---------|--------|
| 当前静态配置 | 低 | MVP | ❌ | ⚠️ 技术债务 |
| 阶段1: 健康检查 | 极低 | 过渡 | ❌ | ✅ 立即实施 |
| 阶段2: PostgreSQL 注册 | 中 | 生产 | ✅ | ⭐⭐⭐ 强烈推荐 |
| Consul/Etcd | 高 | 大规模 | ✅ | ❌ 暂不需要 |

---

## 未来扩展功能

### 1. API 版本管理

支持多个 API 版本并存，确保向后兼容性。

```
/api/v1/manager/engines  → Manager v1
/api/v2/manager/engines  → Manager v2
```

**应用场景**：
- API 升级时不影响旧客户端
- 逐步迁移用户到新版本
- A/B 测试新功能

**实施策略**：
- URL 路径包含版本号（`/v1`、`/v2`）
- 版本号存储在 `service_registry` 表的 metadata 字段
- Gateway 根据版本号路由到不同服务实例

---

### 2. GraphQL 网关集成

提供统一的 GraphQL 入口，聚合多个后端服务。

```
POST /graphql
{
  "query": "{ engines { id name } users { id username } }"
}
```

**应用场景**：
- 前端按需获取数据，减少网络请求
- 聚合查询（一次请求获取多个服务数据）
- 灵活的数据查询语言

**技术方案**：
- 使用 GraphQL Go 库（如：graphql-go、gqlgen）
- 定义统一的 Schema
- 实现 Resolver，调用后端服务 REST API

**实施优先级**：P3

---

### 3. WebSocket 代理支持

支持 WebSocket 连接代理，实现实时数据推送。

```
wss://gateway.example.com/ws/realtime
  → ws://manager:8081/ws/realtime
```

**应用场景**：
- 实时地图更新
- 任务进度推送
- 日志流式输出

**技术方案**：
- Gateway 使用 `gorilla/websocket` 库
- 透明代理 WebSocket 连接
- 支持连接复用和负载均衡

**实施优先级**：P2

---

### 4. gRPC 网关支持

支持 gRPC 协议转换，提供高性能的服务间通信。

```
gRPC Client → Gateway (gRPC-HTTP Transcoding) → HTTP Backend
```

**应用场景**：
- 内部服务使用 gRPC 通信（高性能）
- 外部客户端使用 HTTP/REST 访问
- 微服务架构下的服务间通信

**技术方案**：
- 使用 `grpc-gateway` 库
- 从 gRPC 定义自动生成 REST API
- Gateway 支持双协议（HTTP + gRPC）

**实施优先级**：P3

---

### 5. 熔断降级

后端服务故障时自动熔断，保护系统稳定性。

```
Manager 服务故障
  → 熔断器打开
  → 返回降级响应（缓存数据或默认值）
```

**应用场景**：
- 防止级联故障（雪崩效应）
- 服务降级，保证核心功能可用

**技术方案**：
- 使用 Hystrix 模式或自实现熔断器
- 监控服务失败率，超过阈值自动熔断
- 半开状态：定期尝试恢复

**实施优先级**：P2

---

### 6. 负载均衡

支持多个后端实例的负载均衡，提高系统可用性和性能。

```
Gateway → Manager Instance 1 (Round Robin)
        → Manager Instance 2
        → Manager Instance 3
```

**负载均衡算法**：
- **轮询（Round Robin）**：请求依次分配到各实例
- **加权轮询（Weighted Round Robin）**：根据实例权重分配
- **最少连接（Least Connections）**：优先分配到连接数少的实例
- **一致性哈希（Consistent Hashing）**：同一用户/租户路由到同一实例（会话保持）

**实施优先级**：P2

---

### 7. 智能路由

基于请求特征的智能路由，优化资源利用。

```
大文件上传 → Manager 高性能实例（SSD、大内存）
普通查询 → Manager 普通实例
```

**路由策略**：
- 根据请求大小路由（如：文件上传、大数据查询）
- 根据用户等级路由（如：VIP 用户使用专用实例）
- 根据地域路由（如：就近路由）

**实施优先级**：P3

---

### 8. API 文档聚合

聚合所有服务的 API 文档，提供统一的 API 浏览界面。

```
GET /api-docs  → Swagger UI（所有服务的 API）
```

**功能点**：
- 从各模块自动收集 OpenAPI/Swagger 定义
- 聚合为统一的 API 文档
- 提供在线测试功能（Swagger UI）

**技术方案**：
- 各模块暴露 `/swagger.json` 端点
- Gateway 定期拉取并聚合
- 使用 Swagger UI 展示

**实施优先级**：P2

---

## 总结

### 实施优先级汇总

| 功能 | 优先级 | 预计工作量 | 主要收益 |
|------|--------|-----------|---------|
| Service 数据服务鉴权限流（方案1） | P1 | 2-3 周 | 完善服务安全性，支持公开服务 |
| 阶段1: 健康检查 | P0 | 1-2 天 | 避免路由到宕机服务 |
| 阶段2: PostgreSQL 服务注册 | P1 | 1-2 周 | 动态路由，支持扩容 |
| 实时限流监控界面 | P2 | 1 周 | 可视化限流状态 |
| 动态调整限流阈值 | P2 | 3-5 天 | 无需重启即可调整配置 |
| WebSocket 代理 | P2 | 1 周 | 支持实时数据推送 |
| API 文档聚合 | P2 | 3-5 天 | 统一 API 文档入口 |
| 熔断降级 | P2 | 1 周 | 防止级联故障 |
| 负载均衡 | P2 | 1-2 周 | 支持多实例，提高可用性 |
| 细粒度限流 | P3 | 1 周 | 按服务/租户/IP 限流 |
| GraphQL 网关 | P3 | 2-3 周 | 灵活的数据查询 |
| gRPC 支持 | P3 | 2-3 周 | 高性能服务间通信 |
| 智能路由 | P3 | 1-2 周 | 优化资源利用 |

### 推荐实施路线图

**Q1（第1季度）**
- ✅ 阶段1: 健康检查（P0）
- ✅ Service 数据服务鉴权限流（P1）
- ✅ 阶段2: PostgreSQL 服务注册（P1）

**Q2（第2季度）**
- ⚪ 实时限流监控界面（P2）
- ⚪ 动态调整限流阈值（P2）
- ⚪ WebSocket 代理（P2）

**Q3（第3季度）**
- ⚪ 熔断降级（P2）
- ⚪ 负载均衡（P2）
- ⚪ API 文档聚合（P2）

**Q4（第4季度）**
- ⚪ 细粒度限流（P3）
- ⚪ GraphQL 网关（P3）
- ⚪ 其他扩展功能（按需）
