# Docker Swarm 对 ADDP 工程的价值与作用分析

## 📊 ADDP 工程架构概览

### 一、架构层次

ADDP（全域数据平台）采用**微服务架构**，分为两层部署：

#### **第一层：基础设施层** (docker-compose.infra.yml)
```
4 个核心基础设施服务:
├── PostgreSQL 15 (带 PostGIS 扩展) - 系统元数据存储
├── Redis 7 (Alpine) - 缓存和任务队列
├── MinIO (Latest) - 对象存储服务
└── Meilisearch 1.7 - 全文搜索引擎
```

#### **第二层：应用服务层** (docker-compose.yml)
```
19 个应用服务，分为 7 类:

1️⃣ 系统模块 (System)
   ├── system-backend (端口 8080) - 认证中心 + 配置中心
   └── system-frontend (端口 8090) - 系统管理 UI

2️⃣ 数据管理模块 (Manager)
   ├── manager-backend (端口 8081) - 数据源连接管理
   ├── manager-frontend (端口 8091) - 数据源配置 UI
   └── manager-worker - 后台任务处理器

3️⃣ 元数据模块 (Meta)
   ├── meta-backend (端口 8082) - 元数据扫描 + 血缘追踪
   ├── meta-frontend (端口 8092) - 元数据管理 UI
   └── meta-worker - 自动扫描任务处理器

4️⃣ 数据传输模块 (Transfer)
   ├── transfer-backend (端口 8083) - 数据传输任务管理
   ├── transfer-frontend (端口 8093) - 传输任务 UI
   └── transfer-worker - 异步数据传输处理器

5️⃣ 编排模块 (Orchestrator)
   ├── orchestrator-backend (端口 8084) - 工作流编排
   └── orchestrator-frontend (端口 8094) - 工作流设计 UI

6️⃣ 开发模块 (Develop)
   ├── develop-backend (端口 8085) - SQL 查询执行
   ├── develop-frontend (端口 8095) - SQL 开发工作台
   └── geopandas-engine (端口 8099) - 空间计算引擎 (Python)

7️⃣ 网关与门户
   ├── gateway (端口 8000) - API 路由网关
   ├── portal (端口 5170) - 统一门户前端
   └── nginx (端口 80) - Nginx 统一网关
```

### 二、服务依赖关系图

```
Infrastructure Layer (docker-compose.infra.yml)
  ├── PostgreSQL
  ├── Redis
  ├── MinIO
  └── Meilisearch
       ↓
System Backend (配置中心 + 认证)
  ├── 提供 JWT_SECRET, ENCRYPTION_KEY
  ├── 提供资源配置
  └── 认证所有请求
       ↓
Business Backends (并行启动)
  ├── Manager Backend
  ├── Meta Backend
  ├── Transfer Backend
  ├── Orchestrator Backend
  └── Develop Backend
       ↓
Workers (异步任务)
  ├── manager-worker (MinIO 瓦片缓存)
  ├── meta-worker (元数据扫描)
  └── transfer-worker (数据传输)
       ↓
Gateway (API 路由)
       ↓
Frontends + Portal + Nginx (统一门户)
```

---

## ✅ Docker Swarm 的核心价值

### 1. **解决当前的单点故障风险**

**现状痛点**：
- 所有后端服务（System、Manager、Meta 等）都是单容器部署
- Worker 服务（transfer-worker、meta-worker、manager-worker）无法横向扩展
- 任何一个容器崩溃都会导致服务中断

**Swarm 解决方案**：
```yaml
# 启用多副本 + 自动故障恢复
deploy:
  replicas: 2  # 双副本高可用
  restart_policy:
    condition: any  # 任何失败都自动重启
    delay: 5s
    max_attempts: 3
    window: 120s
```

**效果**：
- ✅ 容器崩溃时自动启动新副本（秒级恢复）
- ✅ 多副本间自动负载均衡
- ✅ 用户无感知故障切换

### 2. **提供零停机更新能力**

**现状痛点**：
- 使用 `docker-compose restart` 会导致服务中断
- 更新需要手动停止再启动

**Swarm 解决方案**：
```yaml
deploy:
  update_config:
    parallelism: 1      # 逐个更新容器
    delay: 10s          # 更新间隔
    failure_action: rollback  # 失败自动回滚
    order: start-first   # 先启动新容器再停止旧容器
```

**更新流程**：
```bash
# 发布新版本（零停机）
docker service update --image addp-transfer-worker:v2.0 addp_transfer-worker

# Swarm 自动执行：
# 1. 启动新版本容器
# 2. 等待健康检查通过
# 3. 停止旧版本容器
# 4. 重复直到所有副本更新完成
# 5. 如果失败，自动回滚到旧版本
```

### 3. **内置负载均衡**

**现状痛点**：
- Gateway 需要手动配置负载均衡
- Worker 服务无法分散任务负载

**Swarm 解决方案**：
- **自动负载均衡**：Swarm 在多个副本间自动分发请求
- **健康检查驱动**：不健康的容器自动剔除，流量路由到健康副本
- **DNS 轮询**：服务名自动解析为多个容器 IP

**示例**：
```yaml
gateway:
  deploy:
    replicas: 2  # 两个 Gateway 实例
  # Nginx 请求自动分发到两个实例
```

### 4. **资源管理和隔离**

**现状痛点**：
- 没有 CPU/内存限制，容器可能耗尽主机资源
- Worker 处理大任务时可能影响其他服务

**Swarm 解决方案**：
```yaml
resources:
  limits:
    cpus: '2'        # 最多使用 2 核
    memory: 2G       # 最多使用 2GB 内存
  reservations:
    cpus: '0.5'      # 保证 0.5 核
    memory: 512M     # 保证 512MB 内存
```

**效果**：
- ✅ 防止单个容器耗尽主机资源
- ✅ 保证关键服务的最低资源需求
- ✅ Swarm 调度器根据资源预留智能分配节点

---

## 🎯 对 ADDP 的具体作用

### 作用 1：**Worker 服务的横向扩展**

ADDP 有 3 个关键 Worker 服务：
- **transfer-worker**：数据传输任务处理（Asynq 队列）
- **meta-worker**：元数据扫描任务（定时任务 + 事件触发）
- **manager-worker**：MVT 瓦片缓存生成（按需处理）

**Swarm 方案**：
```bash
# 动态扩展 Worker 副本
docker service scale addp_transfer-worker=3  # 3 个副本并行处理任务
docker service scale addp_meta-worker=2      # 2 个副本并行扫描
docker service scale addp_manager-worker=2   # 2 个副本并行生成瓦片
```

**效果**：
- ✅ 任务吞吐量成倍提升（3 个 Worker = 3 倍处理能力）
- ✅ 单个 Worker 崩溃不影响整体处理能力
- ✅ 高峰期可临时扩容，低峰期可缩容

**实际场景示例**：
```
场景：用户上传 100 个 Shapefile 文件，触发元数据扫描任务

Compose 模式：
  meta-worker (单副本) 串行处理 → 耗时 100 分钟

Swarm 模式：
  meta-worker (3 副本) 并行处理 → 耗时 35 分钟
  - Worker 1 处理 35 个文件
  - Worker 2 处理 35 个文件
  - Worker 3 处理 30 个文件
```

### 作用 2：**Backend 服务的高可用**

ADDP 有 6 个后端服务，其中 **System Backend** 是配置中心和认证中心，必须高可用。

**Swarm 方案**：
```yaml
system-backend:
  deploy:
    replicas: 2  # 双活模式
    restart_policy:
      condition: any
```

配合 Gateway 或 Nginx 负载均衡：
```nginx
upstream system-backend {
    server system-backend:8080 max_fails=3 fail_timeout=30s;
    # Swarm 自动路由到多个副本
}
```

**效果**：
- ✅ System Backend 崩溃时，另一个副本继续服务
- ✅ 请求自动分发到健康副本
- ✅ 认证和配置服务不间断

### 作用 3：**自动故障恢复**

**场景示例**：
```
场景：transfer-worker 在处理 10GB 大文件时 OOM 崩溃

Compose 模式：
  1. 容器退出
  2. restart: unless-stopped 触发本地重启
  3. 如果宿主机挂了，服务彻底中断

Swarm 模式：
  1. Swarm 检测到容器退出（健康检查失败）
  2. 立即在另一个节点启动新副本（5 秒内）
  3. 任务队列中的任务由新副本继续处理
  4. 用户无感知，任务继续执行
```

**健康检查配置**：
```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:8083/health"]
  interval: 10s
  timeout: 5s
  retries: 3
  start_period: 40s

deploy:
  restart_policy:
    condition: any  # 健康检查失败自动重启
```

---

## 🔍 当前高可用性需求分析

| 服务 | 副本数 | 高可用性 | 状态 | Swarm 建议 |
|------|--------|---------|------|-----------|
| system-backend | 1 | ❌ 单点 | 生产瓶颈 | replicas: 2 |
| manager-backend | 1 | ❌ 单点 | 生产瓶颈 | replicas: 2 |
| manager-worker | 1 | ❌ 单点 | 可扩展 | replicas: 2 |
| meta-backend | 1 | ❌ 单点 | 生产瓶颈 | replicas: 2 |
| meta-worker | 1 | ❌ 单点 | 可扩展 | replicas: 2 |
| transfer-backend | 1 | ❌ 单点 | 生产瓶颈 | replicas: 2 |
| transfer-worker | 1 | ❌ 单点 | 可扩展 | replicas: 3 (高负载) |
| gateway | 1 | ❌ 单点 | 关键瓶颈 | replicas: 2 |
| PostgreSQL | 1 | ❌ 单点 | 数据库单点 | 需专用 HA 方案 |
| Redis | 1 | ❌ 单点 | 缓存单点 | 需 Sentinel/Cluster |
| MinIO | 1 | ❌ 单点 | 存储单点 | 需分布式部署 |

**关键发现**：
- ⚠️ 所有后端服务和基础设施都是单点部署
- ⚠️ 容器崩溃时无法自动恢复到新实例
- ⚠️ 生产环境存在单点故障风险

---

## ⚠️ Swarm 的局限性（需要注意）

### 1. **基础设施层无法解决**

PostgreSQL、Redis、MinIO 的高可用不是 Swarm 能解决的：

**PostgreSQL 高可用方案**：
- **Patroni + etcd**：自动故障切换的主从复制
- **Stolon**：基于 Consul/etcd 的 PostgreSQL HA
- **云服务**：RDS、CloudSQL 等托管数据库

**Redis 高可用方案**：
- **Sentinel 模式**：主从复制 + 自动故障转移
- **Cluster 模式**：分片集群（适合大数据量）
- **云服务**：ElastiCache、阿里云 Redis 等

**MinIO 高可用方案**：
- **分布式部署**：至少 4 节点纠删码模式
- **云服务**：S3、OSS 等对象存储

**为什么 Swarm 无法解决？**
- 数据库需要数据复制和一致性协议（Raft、Paxos）
- Swarm 只能提供容器级别的故障恢复，无法处理数据一致性

### 2. **单机 Swarm 价值有限**

如果只在单台机器上运行 Swarm：
- ✅ 仍然有自动重启和资源管理的价值
- ❌ 无法解决宿主机故障（机器宕机所有服务都挂）
- ❌ 无法实现真正的高可用（单点故障在主机层面）

**建议**：生产环境至少 3 台机器组成 Swarm 集群

**多机 Swarm 架构**：
```
Manager Node 1 (主管理节点)
  ├── etcd (集群元数据)
  └── Swarm Manager

Manager Node 2 (备用管理节点)
  └── Swarm Manager

Worker Node 1-N
  └── 运行应用容器

部署方式：
docker swarm init --advertise-addr <MANAGER_IP>  # 节点 1
docker swarm join-token manager                   # 获取 token
docker swarm join --token <TOKEN> <MANAGER_IP>    # 节点 2/3 加入
```

### 3. **有状态服务的复杂性**

Worker 服务如果涉及本地状态（如临时文件），需要：

**问题示例**：
```
transfer-worker 处理任务时：
1. 从源数据库读取数据 → 写入临时文件 /tmp/data.csv
2. 容器崩溃，Swarm 在另一个节点启动新副本
3. 新副本无法访问 /tmp/data.csv（在旧节点上）
4. 任务失败
```

**解决方案**：

**方案 1：共享存储**
```yaml
volumes:
  - type: volume
    source: shared-storage
    target: /tmp
    volume:
      nocopy: true
      driver: nfs
      driver_opts:
        share: "nfs-server:/export/shared"
```

**方案 2：完全无状态（推荐）**
```go
// 不使用本地文件，直接内存传输
func ProcessTransferTask(task *Task) error {
    // 读取数据到内存
    data, err := readFromSource(task.SourceID)

    // 直接写入目标（不落盘）
    err = writeToTarget(task.TargetID, data)

    return err
}
```

**ADDP Worker 当前状态**：
- transfer-worker：Asynq 队列驱动，**无状态**（✅ 适合 Swarm）
- meta-worker：扫描结果直接写 PostgreSQL，**无状态**（✅ 适合 Swarm）
- manager-worker：瓦片缓存写 MinIO，**无状态**（✅ 适合 Swarm）

---

## 🚀 推荐的实施路径

### **阶段 1：启用 Worker 多副本（低风险，立即见效）**

**目标**：让 Worker 服务获得横向扩展能力

**步骤**：

1. **初始化 Swarm（单机模式）**
   ```bash
   docker swarm init
   ```

2. **取消注释 docker-compose.yml 中的 deploy 配置**
   ```yaml
   # 找到 transfer-worker、meta-worker、manager-worker
   # 取消注释 deploy 部分
   transfer-worker:
     deploy:
       replicas: 2  # 改为 2 或 3
       restart_policy:
         condition: any
       resources:
         limits:
           cpus: '2'
           memory: 2G
   ```

3. **部署到 Swarm**
   ```bash
   docker stack deploy -c docker-compose.yml addp
   ```

4. **验证 Worker 多副本**
   ```bash
   docker service ls
   docker service ps addp_transfer-worker  # 应该看到 2 个副本
   docker service logs -f addp_transfer-worker
   ```

5. **测试扩缩容**
   ```bash
   # 高峰期扩容
   docker service scale addp_transfer-worker=5

   # 低峰期缩容
   docker service scale addp_transfer-worker=2
   ```

**预期价值**：
- ✅ Worker 任务处理能力提升 2-3 倍
- ✅ 单个 Worker 崩溃不影响整体
- ✅ 风险低（Worker 是无状态的）

**适用场景**：
- 开发环境：验证 Swarm 功能
- 测试环境：压力测试和故障演练
- 生产环境（单机）：提升任务处理能力

---

### **阶段 2：Backend 服务多副本（中风险，需要负载均衡）**

**目标**：关键后端服务获得高可用能力

**步骤**：

1. **确认前提条件**
   - ✅ Gateway 或 Nginx 已配置负载均衡
   - ✅ 后端服务是无状态的（Session 存 Redis）
   - ✅ 数据库连接池配置合理

2. **启用关键后端多副本**
   ```yaml
   system-backend:
     deploy:
       replicas: 2
       update_config:
         parallelism: 1
         order: start-first

   gateway:
     deploy:
       replicas: 2
   ```

3. **配置 Nginx 负载均衡**
   ```nginx
   upstream system-backend {
       server system-backend:8080 max_fails=3 fail_timeout=30s;
       # Swarm 自动解析为多个容器 IP
       keepalive 32;
   }

   server {
       location /api/auth/ {
           proxy_pass http://system-backend;
           proxy_next_upstream error timeout http_500 http_502 http_503;
       }
   }
   ```

4. **验证负载均衡**
   ```bash
   # 查看请求分发到哪个容器
   docker service logs -f addp_system-backend

   # 测试故障切换
   docker kill <container-id>  # 杀掉一个副本
   # 验证请求仍然正常（路由到另一个副本）
   ```

**预期价值**：
- ✅ System Backend 高可用（认证不中断）
- ✅ 零停机更新（滚动更新）
- ✅ 请求自动分发到健康副本

**风险点**：
- ⚠️ 如果 Session 存在容器本地，会导致登录失效（需改用 Redis Session）
- ⚠️ 数据库连接数翻倍（需调整连接池配置）

---

### **阶段 3：多机 Swarm 集群（高可用，生产推荐）**

**目标**：实现真正的高可用（跨主机故障恢复）

**步骤**：

1. **准备 3 台机器**
   ```
   node1: 192.168.1.10 (Manager + Worker)
   node2: 192.168.1.11 (Manager + Worker)
   node3: 192.168.1.12 (Worker)
   ```

2. **初始化 Swarm 集群**
   ```bash
   # 在 node1 上执行
   docker swarm init --advertise-addr 192.168.1.10

   # 获取 Manager Token
   docker swarm join-token manager

   # 在 node2 上执行
   docker swarm join --token <MANAGER_TOKEN> 192.168.1.10:2377

   # 获取 Worker Token
   docker swarm join-token worker

   # 在 node3 上执行
   docker swarm join --token <WORKER_TOKEN> 192.168.1.10:2377
   ```

3. **验证集群状态**
   ```bash
   docker node ls
   # 应该看到 3 个节点，其中 2 个 Manager
   ```

4. **部署到集群**
   ```bash
   docker stack deploy -c docker-compose.yml addp
   ```

5. **配置节点亲和性（可选）**
   ```yaml
   # 将数据库固定到特定节点
   postgres:
     deploy:
       placement:
         constraints:
           - node.hostname == node1

   # Worker 分散到所有节点
   transfer-worker:
     deploy:
       replicas: 3
       placement:
         max_replicas_per_node: 1  # 每个节点最多 1 个副本
   ```

6. **测试故障恢复**
   ```bash
   # 模拟 node3 宕机
   ssh node3 "sudo systemctl stop docker"

   # 观察 Swarm 自动将容器调度到 node1/node2
   docker service ps addp_transfer-worker
   ```

**预期价值**：
- ✅ 单台机器故障不影响服务
- ✅ 容器自动调度到健康节点
- ✅ 生产级高可用架构

**前提条件**：
- ✅ 共享存储（NFS、GlusterFS）或无状态架构
- ✅ 数据库高可用（Patroni、RDS）
- ✅ 网络互通（防火墙开放 2377、7946、4789 端口）

---

### **阶段 4：基础设施高可用（完整方案）**

**目标**：解决数据库、缓存、存储的单点故障

**PostgreSQL 高可用方案**：

**方案 1：Patroni + etcd**
```yaml
# docker-compose.patroni.yml
version: '3.8'
services:
  etcd:
    image: quay.io/coreos/etcd:v3.5.0
    deploy:
      replicas: 3

  patroni1:
    image: patroni/patroni:latest
    environment:
      PATRONI_NAME: patroni1
      PATRONI_SCOPE: addp-postgres
      PATRONI_ETCD_HOSTS: etcd:2379

  patroni2:
    image: patroni/patroni:latest
    # 类似配置

  haproxy:
    image: haproxy:2.8
    # 代理读写分离
```

**方案 2：云服务（推荐）**
```yaml
# 使用云数据库 RDS
# 修改 .env
POSTGRES_HOST=addp-rds.xxxxx.rds.aliyuncs.com
POSTGRES_PORT=5432
```

**Redis 高可用方案**：

**Sentinel 模式**
```yaml
redis-master:
  image: redis:7-alpine
  command: redis-server --appendonly yes

redis-replica:
  image: redis:7-alpine
  command: redis-server --slaveof redis-master 6379
  deploy:
    replicas: 2

redis-sentinel:
  image: redis:7-alpine
  command: redis-sentinel /etc/redis/sentinel.conf
  deploy:
    replicas: 3
```

**MinIO 高可用方案**：

**分布式部署（4 节点纠删码）**
```yaml
minio:
  image: minio/minio:latest
  command: server http://minio{1...4}/data{1...2}
  deploy:
    replicas: 4
    placement:
      max_replicas_per_node: 1
  volumes:
    - /mnt/disk1:/data1
    - /mnt/disk2:/data2
```

---

## 📋 快速命令参考

### Swarm 集群管理

```bash
# 初始化 Swarm
docker swarm init

# 查看集群节点
docker node ls

# 查看节点详情
docker node inspect <node-id>

# 将节点设置为维护模式（停止调度）
docker node update --availability drain <node-id>

# 恢复节点
docker node update --availability active <node-id>

# 离开 Swarm
docker swarm leave --force
```

### 服务部署和管理

```bash
# 部署 Stack
docker stack deploy -c docker-compose.yml addp

# 查看 Stack 列表
docker stack ls

# 查看 Stack 中的服务
docker stack services addp

# 查看服务列表
docker service ls

# 查看服务详情
docker service inspect addp_transfer-worker

# 查看服务副本分布
docker service ps addp_transfer-worker

# 查看服务日志
docker service logs -f addp_transfer-worker

# 删除 Stack
docker stack rm addp
```

### 服务扩缩容

```bash
# 扩容服务
docker service scale addp_transfer-worker=5

# 缩容服务
docker service scale addp_transfer-worker=2

# 同时扩缩多个服务
docker service scale \
  addp_transfer-worker=3 \
  addp_meta-worker=2 \
  addp_manager-worker=2
```

### 服务更新

```bash
# 更新镜像（零停机）
docker service update --image addp-transfer-worker:v2.0 addp_transfer-worker

# 更新环境变量
docker service update --env-add NEW_VAR=value addp_transfer-worker

# 回滚到上一个版本
docker service rollback addp_transfer-worker

# 强制重新部署（不改变配置）
docker service update --force addp_transfer-worker
```

### 监控和故障排查

```bash
# 查看服务事件
docker service logs -f addp_transfer-worker

# 查看容器分布
docker service ps addp_transfer-worker --no-trunc

# 查看失败的任务
docker service ps --filter "desired-state=shutdown" addp_transfer-worker

# 查看资源使用
docker stats $(docker ps -q)

# 查看 Swarm 网络
docker network ls
docker network inspect ingress
```

---

## 📊 Swarm vs Compose 对比

| 特性 | Docker Compose | Docker Swarm |
|------|----------------|--------------|
| **部署模式** | 单机 | 单机或多机集群 |
| **副本数** | 1 个容器 | 可配置多副本（replicas: N） |
| **故障恢复** | 本地重启（restart: unless-stopped） | 自动在其他节点启动新副本 |
| **负载均衡** | 需手动配置 Nginx/HAProxy | 内置负载均衡（DNS 轮询） |
| **滚动更新** | ❌ 不支持（需手动停止再启动） | ✅ 支持零停机更新 |
| **资源限制** | 通过 deploy.resources 配置（仅提示） | 强制执行 CPU/内存限制 |
| **健康检查** | 支持，但不触发重新调度 | 健康检查失败自动重新调度 |
| **扩缩容** | 需修改 docker-compose.yml | `docker service scale` 即时生效 |
| **配置方式** | docker-compose.yml | 同一文件（Swarm 读 deploy 字段） |
| **适用场景** | 开发/测试环境 | 生产环境 |

**关键区别**：
- Compose 的 `restart: unless-stopped` 只能**本地重启**（容器在哪个节点就在哪重启）
- Swarm 的 `restart_policy: any` 可以**跨节点重启**（节点挂了就换个节点启动）

---

## 🎯 总结与建议

### **Docker Swarm 对 ADDP 的价值**

| 价值点 | 影响 | 优先级 |
|--------|------|--------|
| **Worker 横向扩展** | 任务处理能力提升 2-5 倍 | ⭐⭐⭐⭐⭐ |
| **自动故障恢复** | 容器崩溃自动重启（秒级） | ⭐⭐⭐⭐⭐ |
| **零停机更新** | 生产发布无需停机 | ⭐⭐⭐⭐ |
| **资源隔离** | 防止单容器耗尽主机资源 | ⭐⭐⭐⭐ |
| **负载均衡** | 请求自动分发到健康副本 | ⭐⭐⭐ |
| **多机高可用** | 跨主机故障恢复 | ⭐⭐⭐ (需多机) |

### **实施建议**

#### **立即启用（低风险，高收益）**
```bash
# 1. 初始化 Swarm
docker swarm init

# 2. 取消注释 docker-compose.yml 中 Worker 的 deploy 配置

# 3. 部署
docker stack deploy -c docker-compose.yml addp

# 4. 扩容 Worker
docker service scale addp_transfer-worker=3
```

**适用场景**：
- ✅ 开发环境：验证功能
- ✅ 测试环境：压力测试
- ✅ 生产环境（单机）：提升任务处理能力

#### **谨慎启用（需要配套改造）**
- **Backend 多副本**：需配合 Nginx 负载均衡 + Redis Session
- **多机集群**：需配合共享存储或完全无状态架构
- **基础设施 HA**：需要专用高可用方案（Patroni、Sentinel、MinIO 分布式）

#### **不推荐的场景**
- ❌ 纯开发环境（单机单副本 Compose 已够用）
- ❌ 数据库高可用（需要专用方案，Swarm 无法解决）
- ❌ 有状态服务且无共享存储

### **ADDP 当前状态评估**

**✅ 适合立即启用 Swarm 的服务**：
- transfer-worker（无状态，任务驱动）
- meta-worker（无状态，扫描结果写 DB）
- manager-worker（无状态，瓦片写 MinIO）

**⚠️ 需要改造后启用的服务**：
- system-backend（需 Redis Session + Nginx 负载均衡）
- gateway（需多副本 + Nginx 负载均衡）

**❌ 暂不建议启用 Swarm 的服务**：
- PostgreSQL（需 Patroni 等专用 HA 方案）
- Redis（需 Sentinel/Cluster 模式）
- MinIO（需分布式部署）

### **最终建议**

**阶段性实施路线图**：

```
第 1 周：启用 Worker 多副本（立即见效）
  └─ docker service scale addp_transfer-worker=3

第 2 周：验证故障恢复和扩缩容
  └─ 压力测试 + 故障演练

第 3-4 周：Backend 多副本改造（可选）
  ├─ 改用 Redis Session
  └─ 配置 Nginx 负载均衡

第 2-3 个月：多机 Swarm 集群（生产推荐）
  ├─ 准备 3 台机器
  └─ 配置共享存储

第 3-6 个月：基础设施高可用（完整方案）
  ├─ PostgreSQL Patroni 集群
  ├─ Redis Sentinel 模式
  └─ MinIO 分布式部署
```

**核心原则**：
- ✅ 先启用低风险的 Worker 多副本（立即获得价值）
- ✅ 逐步改造 Backend 服务（分阶段降低风险）
- ✅ 最后解决基础设施高可用（需要专业方案）

---

## 📚 参考资源

- [Docker Swarm 官方文档](https://docs.docker.com/engine/swarm/)
- [Docker Stack 部署指南](https://docs.docker.com/engine/reference/commandline/stack_deploy/)
- [ADDP 生产部署脚本](../scripts/prod/README.md)
- [ADDP 架构文档](../CLAUDE.md)
