# Docker Swarm 高可用部署指南

本文档说明如何使用 Docker Swarm 模式部署 ADDP 平台，实现 Transfer Worker 的高可用性。

## 概述

### 为什么使用 Docker Swarm？

Docker Swarm 是 Docker 内置的容器编排工具，提供：
- ✅ **自动故障恢复**：容器崩溃时自动启动新副本，维持设定的副本数
- ✅ **内置负载均衡**：多个副本间自动分配请求
- ✅ **滚动更新**：零停机更新服务
- ✅ **健康检查**：基于健康状态自动替换不健康的容器
- ✅ **资源限制**：CPU 和内存配额管理

### 架构对比

#### Compose 模式（开发环境）
```
docker-compose up -d
  ↓
启动单个 transfer-worker 容器
  ↓
容器崩溃 → 重启原容器（restart: unless-stopped）
  ↓
如果重启失败 → 服务不可用 ❌
```

#### Swarm 模式（生产环境）
```
docker stack deploy -c docker-compose.prod.yml addp
  ↓
启动 2 个 transfer-worker 副本（replicas: 2）
  ↓
副本 1 崩溃 → Swarm 自动启动新副本 1
              ↓
           副本 2 继续工作 ✅（无服务中断）
              ↓
           维持 2 个副本运行
```

---

## 部署步骤

### 前置条件

- Docker Engine 19.03+
- 单机或多机 Linux 服务器
- 已构建好的 ADDP 镜像

### 步骤 1：初始化 Docker Swarm

#### 单机部署
```bash
# 初始化 Swarm（自动成为 Manager 节点）
docker swarm init

# 输出示例：
# Swarm initialized: current node (abc123) is now a manager.
# To add a worker to this swarm, run the following command:
#     docker swarm join --token SWMTKN-1-xxx... 192.168.1.100:2377
```

#### 多机部署（可选）
```bash
# Manager 节点
docker swarm init --advertise-addr 192.168.1.100

# Worker 节点（在其他机器上执行）
docker swarm join --token SWMTKN-1-xxx... 192.168.1.100:2377

# 查看节点
docker node ls
# ID         HOSTNAME   STATUS   AVAILABILITY   MANAGER STATUS
# abc123 *   manager    Ready    Active         Leader
# def456     worker1    Ready    Active
```

### 步骤 2：准备配置文件

确保 `.env` 文件存在：
```bash
cp .env.example .env
# 编辑 .env 设置密钥和密码
```

### 步骤 3：部署 Stack

```bash
# 部署完整平台（包括 Transfer Worker）
docker stack deploy -c docker-compose.prod.yml addp

# 或者只部署 Transfer 模块
docker stack deploy -c docker-compose.prod.yml --with-registry-auth addp
```

**部署输出**：
```
Creating network addp_addp-network
Creating service addp_postgres
Creating service addp_redis
Creating service addp_minio
Creating service addp_transfer-backend
Creating service addp_transfer-worker  ← 注意：会创建 2 个副本
Creating service addp_transfer-frontend
```

### 步骤 4：验证部署

#### 查看服务列表
```bash
docker service ls

# 输出示例：
# ID          NAME                    MODE        REPLICAS   IMAGE
# abc123      addp_postgres           replicated  1/1        postgis/postgis:15-3.4
# def456      addp_redis              replicated  1/1        redis:6.2.19-alpine
# ghi789      addp_transfer-backend   replicated  1/1        addp-transfer-backend:latest
# jkl012      addp_transfer-worker    replicated  2/2        addp-transfer-worker:latest  ← 2 个副本
```

#### 查看 Transfer Worker 详情
```bash
docker service ps addp_transfer-worker

# 输出示例：
# ID      NAME                     IMAGE                         NODE      DESIRED STATE   CURRENT STATE
# 1a2b    addp_transfer-worker.1   addp-transfer-worker:latest   manager   Running         Running 30 seconds ago
# 3c4d    addp_transfer-worker.2   addp-transfer-worker:latest   manager   Running         Running 30 seconds ago
```

#### 查看实时日志
```bash
# 查看所有 Worker 日志
docker service logs -f addp_transfer-worker

# 查看特定副本日志
docker service logs -f addp_transfer-worker.1
```

---

## 高可用验证

### 测试 1：容器崩溃恢复

```bash
# 1. 查看当前运行的容器
docker ps | grep transfer-worker
# abc123  addp_transfer-worker.1.xxx
# def456  addp_transfer-worker.2.xxx

# 2. 手动杀掉一个容器
docker kill abc123

# 3. 立即查看服务状态（5 秒内）
docker service ps addp_transfer-worker
# ID      NAME                       DESIRED STATE   CURRENT STATE
# 1a2b    addp_transfer-worker.1     Running         Starting 2 seconds ago  ← 新副本启动中
# 5e6f    \_ addp_transfer-worker.1  Shutdown        Failed 3 seconds ago    ← 原副本已关闭
# 3c4d    addp_transfer-worker.2     Running         Running 5 minutes ago   ← 另一个副本正常运行

# 4. 10 秒后再次查看
docker service ps addp_transfer-worker
# ID      NAME                     DESIRED STATE   CURRENT STATE
# 1a2b    addp_transfer-worker.1   Running         Running 8 seconds ago   ← 新副本已运行
# 3c4d    addp_transfer-worker.2   Running         Running 5 minutes ago
```

**结论**：✅ Swarm 自动启动新副本，维持 2 个副本运行

### 测试 2：任务执行不中断

```bash
# 1. 创建 100 个测试任务
for i in {1..100}; do
  curl -X POST http://localhost:8083/api/tasks/1/enqueue \
    -H "Authorization: Bearer $TOKEN" &
done

# 2. 查看队列
redis-cli -a addp_redis LLEN "asynq:{default}:pending"
# 100

# 3. 杀掉一个 Worker
docker ps | grep transfer-worker
docker kill <container_id_1>

# 4. 观察队列继续消费
watch -n 1 'redis-cli -a addp_redis LLEN "asynq:{default}:pending"'
# 100 → 95 → 90 → 85 → ... ← 另一个 Worker 继续工作
#                              新 Worker 启动后一起消费
```

**结论**：✅ 任务执行不中断，零停机时间

### 测试 3：资源限制验证

```bash
# 查看资源使用情况
docker stats $(docker ps -q --filter name=addp_transfer-worker)

# 输出示例：
# CONTAINER ID   CPU %     MEM USAGE / LIMIT     MEM %
# abc123         45.2%     1.2GB / 2GB          60%     ← 内存限制 2GB
# def456         38.7%     980MB / 2GB          49%
```

---

## 管理命令

### 服务管理

#### 查看服务列表
```bash
docker service ls
```

#### 查看服务详情
```bash
docker service inspect addp_transfer-worker --pretty
```

#### 查看服务日志
```bash
# 所有副本
docker service logs -f addp_transfer-worker

# 最近 100 行
docker service logs --tail 100 addp_transfer-worker

# 指定副本
docker service logs addp_transfer-worker.1
```

#### 手动扩展副本数
```bash
# 扩展到 5 个副本
docker service scale addp_transfer-worker=5

# 缩减到 1 个副本
docker service scale addp_transfer-worker=1

# 恢复默认 2 个副本
docker service scale addp_transfer-worker=2
```

### 更新服务

#### 滚动更新镜像
```bash
# 更新到新版本（零停机）
docker service update \
  --image addp-transfer-worker:v2.0 \
  addp_transfer-worker

# Swarm 会自动：
# 1. 启动新版本容器
# 2. 等待健康检查通过
# 3. 停止旧版本容器
# 4. 逐个更新所有副本
```

#### 更新环境变量
```bash
docker service update \
  --env-add CONCURRENT_TASKS=20 \
  addp_transfer-worker
```

#### 回滚更新
```bash
# 回滚到上一个版本
docker service rollback addp_transfer-worker
```

### 故障排查

#### 查看服务事件
```bash
docker service ps addp_transfer-worker --no-trunc
```

#### 查看失败的任务
```bash
docker service ps addp_transfer-worker --filter "desired-state=shutdown"
```

#### 进入容器调试
```bash
# 找到容器 ID
docker ps | grep transfer-worker

# 进入容器
docker exec -it <container_id> /bin/sh
```

---

## 停止和删除

### 停止服务（保留数据）
```bash
# 删除整个 Stack
docker stack rm addp

# 等待所有容器停止
watch docker ps
```

### 清理 Swarm
```bash
# 如果不再使用 Swarm
docker swarm leave --force
```

---

## Compose 模式 vs Swarm 模式对比

### 使用场景

| 场景 | 推荐模式 | 命令 |
|------|---------|------|
| **本地开发** | Compose | `docker-compose up -d` |
| **CI/CD 测试** | Compose | `docker-compose up -d` |
| **单机生产** | Swarm | `docker stack deploy -c docker-compose.prod.yml addp` |
| **多机集群** | Swarm | `docker stack deploy -c docker-compose.prod.yml addp` |

### 功能对比

| 功能 | Compose 模式 | Swarm 模式 |
|------|-------------|-----------|
| 启动速度 | ✅ 快 | ⚠️ 稍慢（健康检查等待） |
| 多副本 | ⚠️ 需要 `--scale` | ✅ 自动（replicas） |
| 故障恢复 | ⚠️ 重启原容器 | ✅ 启动新副本 |
| 负载均衡 | ❌ 需要 Nginx | ✅ 内置 |
| 滚动更新 | ❌ | ✅ |
| 资源限制 | ⚠️ 基础 | ✅ 完整（limits + reservations） |
| 配置文件 | `docker-compose.yml` | 同一文件（兼容） |

### 配置兼容性

**同一配置文件支持两种模式**：

```yaml
# docker-compose.prod.yml
services:
  transfer-worker:
    image: addp-transfer-worker:latest
    restart: unless-stopped  # ← Compose 模式使用
    deploy:                  # ← Swarm 模式使用
      replicas: 2
      restart_policy: ...
      resources: ...
```

**Compose 模式**：忽略 `deploy` 配置块
**Swarm 模式**：忽略 `restart` 配置

---

## 监控与告警

### Prometheus 监控（推荐）

```yaml
# 添加到 docker-compose.prod.yml
services:
  prometheus:
    image: prom/prometheus
    volumes:
      - ./monitoring/prometheus.yml:/etc/prometheus/prometheus.yml
    ports:
      - "9090:9090"
    deploy:
      placement:
        constraints:
          - node.role == manager
```

```yaml
# monitoring/prometheus.yml
scrape_configs:
  - job_name: 'docker-swarm'
    dockerswarm_sd_configs:
      - host: unix:///var/run/docker.sock
        role: tasks
    relabel_configs:
      - source_labels: [__meta_dockerswarm_service_name]
        target_label: service
```

### Grafana 可视化

```yaml
services:
  grafana:
    image: grafana/grafana
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
    deploy:
      placement:
        constraints:
          - node.role == manager
```

---

## 最佳实践

### 1. 副本数设置

| 场景 | 推荐副本数 |
|------|-----------|
| **开发/测试** | 1 |
| **小规模生产** | 2 |
| **中规模生产** | 3-5 |
| **大规模生产** | 5-10 |

### 2. 资源配额

根据机器配置设置：

**8 核 16GB 机器**：
```yaml
resources:
  limits:
    cpus: '2'      # 每个 Worker 最多 2 核
    memory: 2G     # 每个 Worker 最多 2GB
  reservations:
    cpus: '0.5'
    memory: 512M
# 2 个副本 = 最多 4 核 + 4GB（留余量给其他服务）
```

**16 核 32GB 机器**：
```yaml
resources:
  limits:
    cpus: '4'
    memory: 4G
  reservations:
    cpus: '1'
    memory: 1G
# 可运行 4-5 个副本
```

### 3. 更新策略

```yaml
update_config:
  parallelism: 1         # 一次更新 1 个副本（更安全）
  delay: 10s             # 每个副本间隔 10 秒
  failure_action: rollback  # 失败自动回滚
  order: start-first     # 先启动新容器再停止旧容器（零停机）
```

### 4. 健康检查

在 Dockerfile 中添加：
```dockerfile
HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
  CMD curl -f http://localhost:8080/health || exit 1
```

Swarm 会基于健康检查自动替换不健康的容器。

---

## 故障排查指南

### 问题 1：副本数不足

**现象**：
```bash
docker service ls
# addp_transfer-worker  replicated  1/2  ← 只有 1 个副本运行
```

**排查**：
```bash
docker service ps addp_transfer-worker --no-trunc

# 可能原因：
# 1. 镜像拉取失败
# 2. 资源不足（CPU/内存）
# 3. 健康检查失败
# 4. 依赖服务未就绪
```

**解决**：
```bash
# 查看详细日志
docker service logs addp_transfer-worker

# 检查资源使用
docker node inspect self --pretty | grep Resources
```

### 问题 2：容器频繁重启

**现象**：
```bash
docker service ps addp_transfer-worker
# 显示大量 Shutdown/Failed 记录
```

**排查**：
```bash
# 查看失败原因
docker service ps addp_transfer-worker --no-trunc \
  --filter "desired-state=shutdown"

# 查看日志
docker service logs --tail 50 addp_transfer-worker
```

**可能原因**：
- 数据库连接失败
- Redis 连接失败
- 内存溢出（OOM）
- 配置错误

### 问题 3：无法访问服务

**排查网络**：
```bash
# 检查网络
docker network ls
docker network inspect addp_addp-network

# 检查服务端口
docker service inspect addp_transfer-backend --format '{{.Endpoint.Ports}}'
```

---

## 迁移指南

### 从 Compose 迁移到 Swarm

#### 步骤 1：备份数据
```bash
# 停止服务
docker-compose down

# 备份 volumes
docker run --rm -v addp_postgres_data:/data -v $(pwd):/backup \
  alpine tar czf /backup/postgres-backup.tar.gz -C /data .
```

#### 步骤 2：初始化 Swarm
```bash
docker swarm init
```

#### 步骤 3：部署到 Swarm
```bash
docker stack deploy -c docker-compose.prod.yml addp
```

#### 步骤 4：恢复数据（如果需要）
```bash
docker run --rm -v addp_postgres_data:/data -v $(pwd):/backup \
  alpine tar xzf /backup/postgres-backup.tar.gz -C /data
```

### 从 Swarm 回退到 Compose

```bash
# 停止 Stack
docker stack rm addp

# 离开 Swarm
docker swarm leave --force

# 恢复 Compose 模式
docker-compose up -d
```

---

## 总结

Docker Swarm 为 ADDP Transfer Worker 提供了：
- ✅ **高可用性**：2 个副本同时运行，单点故障不影响服务
- ✅ **自动恢复**：容器崩溃自动启动新副本
- ✅ **零停机更新**：滚动更新不中断任务执行
- ✅ **资源管理**：CPU 和内存配额防止资源耗尽
- ✅ **简单易用**：单机可用，无需复杂配置

**推荐生产环境使用 Swarm 模式，开发环境使用 Compose 模式。**

---

## 相关文档

- [Docker Swarm 官方文档](https://docs.docker.com/engine/swarm/)
- [Docker Stack 部署参考](https://docs.docker.com/engine/reference/commandline/stack_deploy/)
- [CLAUDE.md - ADDP 平台架构](../CLAUDE.md)
- [Transfer Worker 架构](../transfer/README.md)
