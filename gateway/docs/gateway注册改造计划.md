# Gateway 服务注册与发现改造计划

## 📊 现状分析

### 当前架构
```
.env (硬编码服务URL)
  ↓
Gateway 读取配置
  ↓
硬编码路由规则 (router.go:93-142)
  ↓
转发请求到指定模块
```

### 现状核实
1. ✅ `.env` 存储所有模块的服务 URL（如 `SYSTEM_SERVICE_URL=http://localhost:8180`）
2. ✅ Gateway 从 `.env` 读取各模块 URL，硬编码路由规则
3. ✅ 各模块从 `.env` 读取自己的端口配置（如 `SYSTEM_BACKEND_PORT=8180`）
4. ✅ 各模块 API 路径带有模块名前缀（如 `/api/system/users`）
5. ✅ Gateway 基于 **URL 路径前缀匹配** 转发请求（硬编码规则）
6. ❌ **没有服务注册与发现机制**

---

## 🏗️ 架构问题分析

### 当前架构的问题

| 问题 | 影响 | 严重程度 |
|------|------|---------|
| **硬编码路由规则** | Gateway 每增加一个模块都要修改代码，违反开闭原则 | ⚠️ 中 |
| **静态配置，无动态感知** | 模块宕机或扩容，Gateway 无法感知，路由失效 | 🔴 高 |
| **无服务健康检查** | Gateway 可能将请求转发到已宕机的服务 | 🔴 高 |
| **无负载均衡** | 单个模块多实例时，无法自动分配流量 | ⚠️ 中 |
| **启动顺序依赖** | Gateway 必须最后启动，否则读取不到服务信息 | ⚠️ 中 |

### 技术债务风险
- Gateway 的 `router.go:93-142` 硬编码了所有路由规则
- 每新增一个模块，必须修改 Gateway 代码并重新部署
- **违反微服务的"独立部署"原则**

---

## 🎯 业界最佳实践对比

### 微服务网关架构演进

#### 1️⃣ 静态配置模式（当前方案）
```
Gateway 读取 .env → 硬编码路由规则 → 转发请求
```
- **适用场景**: 小型项目（<5 个服务）、固定拓扑、单实例部署
- **ADDP 现状**: ✅ 符合当前规模（7个服务），但 **缺乏扩展性**

#### 2️⃣ 服务注册与发现（推荐方案）
```
服务启动 → 注册到注册中心 → Gateway 订阅服务列表 → 动态路由
```
- **适用场景**: 中型项目（5-20 个服务）、动态扩缩容、多实例部署
- **业界方案**:
  - **Consul** / **Etcd** (独立注册中心，云原生标准)
  - **Redis** (轻量级，ADDP 已有)
  - **数据库** (最简单，适合 ADDP)

#### 3️⃣ Service Mesh（过度设计）
```
Sidecar 代理 + 控制平面（Istio/Linkerd）
```
- **适用场景**: 大型项目（>50 个服务）、Kubernetes 环境
- **ADDP 评估**: ❌ 复杂度过高，不适合

---

## 💡 推荐方案（分阶段实施）

### 阶段 1：最小改动方案（立即可用）
**保留静态配置 + 增加健康检查**

```
Gateway → /health (每 10s 检查)
    ↓
  ✅ 可用 → 路由
  ❌ 宕机 → 503 错误
```

**优点**:
- 无需改动架构，成本极低
- 立即提升系统可靠性

**缺点**:
- 仍然依赖硬编码路由
- 不能解决模块扩容问题

**工作量**: ~50 行代码
**收益**: 避免路由到宕机服务
**优先级**: P0 (本周完成)

---

### 阶段 2：轻量级服务注册（推荐）

#### 架构设计

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

#### 方案 A：基于 PostgreSQL（推荐）

**为什么推荐 PostgreSQL？**
- ✅ ADDP 是数据平台，DB 操作更自然
- ✅ 无需新增外部组件
- ✅ 天然持久化，容错能力强
- ✅ System 已有 PostgreSQL 连接

**数据表设计**

```sql
-- 在 system schema 中创建
CREATE TABLE service_registry (
    id SERIAL PRIMARY KEY,
    service_name VARCHAR(50) UNIQUE NOT NULL,  -- 'system', 'manager', 'meta' 等
    service_url VARCHAR(255) NOT NULL,         -- 'http://localhost:8180'
    status VARCHAR(20) DEFAULT 'up',           -- 'up', 'down'
    last_heartbeat TIMESTAMP DEFAULT NOW(),
    metadata JSONB,                            -- 扩展信息（版本、权重等）
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- 创建索引加速查询
CREATE INDEX idx_service_registry_status ON service_registry(status);
CREATE INDEX idx_service_registry_heartbeat ON service_registry(last_heartbeat);
```

**实现流程**

**1. 各模块启动时注册**
```go
// 各模块 main.go 启动时调用（在服务启动之前）
func registerService(ctx context.Context, systemClient *client.SystemClient) error {
    return systemClient.RegisterService(ctx, &RegisterServiceRequest{
        ServiceName: "system",
        ServiceURL:  "http://localhost:8180",
        Metadata: map[string]interface{}{
            "version": "1.0.0",
            "weight": 100,
        },
    })
}
```

**2. Gateway 动态加载**
```go
// Gateway 启动时 + 定期刷新（每 30s）
func (g *Gateway) refreshServiceRegistry() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        services := systemClient.GetServices(ctx)
        g.updateRouteTable(services)
    }
}

// 路由转发时，动态查表而非硬编码
func (g *Gateway) resolveServiceURL(moduleName string) string {
    return g.routeTable[moduleName].ServiceURL
}
```

**3. 各模块定期发送心跳**
```go
// 各模块定期调用（每 10s）
func sendHeartbeat(ctx context.Context, systemClient *client.SystemClient) {
    systemClient.Heartbeat(ctx, "system")
}
```

**4. Gateway 清理离线服务**
```go
// 定期清理超过 60s 未心跳的服务
SELECT * FROM service_registry
WHERE status = 'up' AND last_heartbeat < NOW() - INTERVAL '60 seconds'
UPDATE service_registry SET status = 'down' WHERE ...
```

---

#### 方案 B：基于 Redis（可选）

```go
// 服务注册到 Redis
HSET gateway:services system "http://localhost:8180"
EXPIRE gateway:services:system:heartbeat 30  // TTL 心跳

// Gateway 订阅 Pub/Sub
SUBSCRIBE gateway:service:update
```

**对比分析**

| 维度 | PostgreSQL | Redis |
|------|-----------|-------|
| 实现复杂度 | 简单（复用现有连接） | 中等（需 Pub/Sub） |
| 性能 | 中（DB 查询） | 高（内存） |
| 数据持久化 | ✅ 天然持久 | ⚠️ 需配置 AOF |
| ADDP 适配 | ✅ 完美融合 | ✅ 已有 Redis |
| 故障恢复 | 强（持久化数据) | 中（需重新注册） |

**结论**: 采用 **PostgreSQL 方案**（最适合 ADDP）

**工作量**: ~200 行代码
**收益**: 动态路由、解耦 Gateway 代码、支持模块扩容
**优先级**: P1 (近期完成，2周内)

---

### 阶段 3：完整服务治理（长期规划）

增加特性：
- ✅ 负载均衡（多实例轮询）
- ✅ 熔断降级（Hystrix 模式）
- ✅ 服务版本管理（灰度发布）
- ✅ 流量统计和分析

**时机**: 服务数量 >15 个或需要灰度发布
**优先级**: P2

---

## 🤔 常见问题解答

### Q1: 注册中心应该放在哪里？

**答案**: **System 模块**（强烈推荐）

**理由**:
1. **职责匹配**: System 已负责用户、租户、引擎管理，服务注册是自然延伸
2. **避免循环依赖**: Gateway 调用 System 获取服务列表，逻辑清晰
3. **复用基础设施**: System 已连接 PostgreSQL，无需新增组件
4. **启动顺序**: System → 其他模块 → Gateway（Gateway 最后启动是合理的）

**不推荐放 Gateway 的原因**:
- Gateway 职责应聚焦路由转发，不应承担数据管理
- 会导致 System 反向依赖 Gateway（架构混乱）

---

### Q2: 启动顺序问题如何解决？

**推荐顺序**:
```
1. PostgreSQL + Redis（基础设施）
2. System 模块（服务注册中心）
3. 其他业务模块（启动时注册到 System）
4. Gateway（读取服务列表，启动路由）
```

**容错机制**:
- Gateway 启动时，若服务列表为空，进入 **降级模式**（使用 `.env` 中的默认配置）
- 定期重试拉取服务列表（每 30s），自动恢复

**启动脚本调整**:
```bash
#!/bin/bash
# scripts/dev/start.sh

# 1. 启动基础设施
docker-compose up -d postgres redis

# 2. 启动 System 模块（注册中心）
./scripts/dev/start.sh -system

# 等待 System 服务就绪
sleep 5

# 3. 启动其他模块（按依赖顺序）
./scripts/dev/start.sh -manager
./scripts/dev/start.sh -meta
# ... 其他模块

# 4. 最后启动 Gateway
./scripts/dev/start.sh -gateway
```

---

### Q3: 是否需要独立的注册中心（Consul/Etcd）？

**答案**: **目前不需要**

**理由**:
- ADDP 当前规模（7 个服务）不需要专用注册中心
- PostgreSQL 方案已能满足：服务注册、健康检查、动态发现
- 减少运维复杂度（避免引入新组件）

**未来考虑 Consul 的场景**:
- 服务数量 >20 个
- Kubernetes 多集群部署
- 需要分布式配置管理
- 需要服务跨机房部署

---

### Q4: 如何避免单点故障（Gateway 无法连接 System）？

**方案**:
1. **多重降级**:
   - 第一层：使用 .env 的默认配置
   - 第二层：定期刷新缓存到本地 JSON 文件
   - 第三层：路由失败时自动重试

2. **System 高可用** (长期):
   - 主从部署
   - 读写分离
   - 自动故障转移

3. **本地缓存策略**:
```go
// Gateway 定期保存服务列表到本地
if serviceRegistry != lastCache {
    writeToFile("./data/service_registry_cache.json", serviceRegistry)
}

// 启动时优先读本地缓存
if err := readFromFile(...); err == nil {
    useCache()
} else {
    useEnvDefault()
}
```

---

### Q5: 如何处理服务多实例的负载均衡？

**阶段 2 方案**（简化版）:
```sql
-- 同一服务的多个实例
INSERT INTO service_registry VALUES
  ('manager', 'http://localhost:8081', 'up', ...),
  ('manager', 'http://manager-2:8081', 'up', ...),  -- 第二个实例
  ('manager', 'http://manager-3:8081', 'up', ...);  -- 第三个实例
```

**Gateway 轮询转发**:
```go
func (g *Gateway) getNextServiceURL(serviceName string) string {
    instances := g.routeTable[serviceName]  // 所有实例
    return instances[g.roundRobin[serviceName]++ % len(instances)].ServiceURL
}
```

**阶段 3 方案**（加权负载均衡）:
- 支持权重配置（metadata.weight）
- 支持自定义负载均衡策略（轮询、最少连接等）

---

## 📋 实施路线图

### 优先级排序

#### P0 - 阶段 1：健康检查心跳（本周完成）
**改动量**: ~50 行代码
**收益**: 避免路由到宕机服务
**实施步骤**:
1. [ ] 为各模块添加定期心跳上报逻辑
2. [ ] Gateway 检查服务可用性
3. [ ] 路由失败时返回 503 和重试提示

---

#### P1 - 阶段 2：PostgreSQL 服务注册（2周内）
**改动量**: ~200 行代码
**收益**: 动态路由、解耦 Gateway 代码
**实施步骤**:
1. [ ] 在 System schema 中创建 `service_registry` 表
2. [ ] System 模块添加服务注册/心跳/查询接口
3. [ ] 各模块在启动时调用注册接口
4. [ ] 各模块定期发送心跳
5. [ ] Gateway 启动时加载服务列表
6. [ ] Gateway 定期刷新服务列表（30s）
7. [ ] Gateway 路由规则改为动态查表
8. [ ] 添加本地缓存降级机制
9. [ ] 更新启动脚本确保顺序正确

**关键文件**:
- `system/backend/internal/repository/service_registry.go` (新增)
- `system/backend/internal/service/service_registry_service.go` (新增)
- `system/backend/internal/api/service_registry.go` (新增)
- `gateway/internal/router/router.go` (修改 - 改为动态路由)
- `gateway/internal/service_discovery.go` (新增)
- 各模块 `main.go` (修改 - 添加启动注册)

---

#### P2 - 阶段 3：完整服务治理（长期）
**时机**: 服务数量 >15 个或需要灰度发布
**特性**:
1. [ ] 负载均衡（多实例轮询、加权）
2. [ ] 熔断降级（Hystrix 模式）
3. [ ] 服务版本管理（灰度发布）
4. [ ] 流量统计和分析

---

## 🔍 影响范围分析

### 需要修改的组件

#### Gateway 模块
- `internal/config/config.go` - 添加服务注册配置
- `internal/router/router.go` - 改为动态路由逻辑
- `internal/service_discovery.go` - 新增服务发现模块
- `internal/cache/registry_cache.go` - 新增本地缓存

#### System 模块
- `internal/models/service_registry.go` - 新增数据模型
- `internal/repository/service_registry.go` - 新增数据访问层
- `internal/service/service_registry_service.go` - 新增业务逻辑
- `internal/api/service_registry.go` - 新增 API 接口

#### 各业务模块 (system/manager/meta/transfer/develop/service/copilot)
- `cmd/server/main.go` - 添加启动注册和心跳逻辑

#### 启动脚本
- `scripts/dev/start.sh` - 调整启动顺序

---

## 🚀 快速开始

### 立即可做的改进
1. **添加服务可用性检查** (无风险)
   - 为 Gateway 添加定期健康检查逻辑
   - 标记不可用的服务，避免路由

2. **优化启动顺序** (低风险)
   - 确保 System 最先启动
   - Gateway 在其他模块之后启动
   - 修改启动脚本 `scripts/dev/start.sh`

3. **为 .env 添加说明** (无风险)
   - 文档说明 `*_SERVICE_URL` 的含义
   - 注明这是临时方案，未来会改为动态注册

---

## 📊 总结对比

| 方案 | 复杂度 | 适用阶段 | 启动顺序 | 扩容支持 | 推荐度 |
|------|--------|---------|---------|---------|--------|
| 当前静态配置 | 低 | MVP | 独立 | ❌ | ⚠️ 技术债务 |
| 阶段1: 健康检查 | 极低 | 过渡 | 独立 | ❌ | ✅ 立即实施 |
| 阶段2: PostgreSQL 注册 | 中 | 生产 | 有顺序 | ✅ | ⭐⭐⭐ 强烈推荐 |
| 阶段2: Redis 注册 | 中 | 生产 | 有顺序 | ✅ | ⭐⭐ 可选 |
| Consul/Etcd | 高 | 大规模 | 严格顺序 | ✅ | ❌ 暂不需要 |

---

## 💬 更新日志

- **2026-01-29**: 初稿完成，包含三阶段改造方案
