# ADDP 生产环境部署指南

本文档说明如何在生产环境中完整部署 ADDP 平台（基础设施 + 后端 + 前端 + Portal 统一入口）。

## 快速开始

### 一键部署（推荐）

```bash
# 从项目根目录执行
./scripts/prod/start.sh
```

该脚本会自动按正确顺序启动所有服务，并进行健康检查。

### 部署流程

脚本会执行以下步骤：

1. **启动基础设施层** (PostgreSQL, Redis, MinIO, Meilisearch)
   - 自动等待所有基础设施服务就绪

2. **启动 System Backend** (配置中心)
   - 其他所有服务依赖 System 提供配置和认证
   - 健康检查确保服务可用后再继续

3. **启动业务后端服务** (并行启动)
   - Manager Backend + Worker
   - Meta Backend + Worker
   - Transfer Backend + Worker
   - Orchestrator Backend
   - Develop Backend
   - Gateway (API 路由器)

4. **启动前端服务和 Portal**
   - 所有模块前端 (System, Manager, Meta, Transfer, Orchestrator, Develop)
   - Portal 统一门户
   - Nginx 统一网关 (80 端口)

5. **健康检查**
   - 验证所有后端服务 `/health` 端点
   - 验证 Portal 和 Nginx 是否正常运行

## 访问地址

部署完成后，系统提供以下访问方式：

### 推荐访问方式（生产环境）

**Portal 统一入口（通过 Nginx）**:
```
http://localhost:80
```

- ✅ 统一登录认证
- ✅ 左侧导航栏一键切换模块
- ✅ 所有模块通过 iframe 无缝集成
- ✅ 最佳用户体验

### 独立访问方式（开发调试）

**Portal 独立访问**:
```
http://localhost:5170
```

**各模块前端**:
- System Frontend: http://localhost:8090
- Manager Frontend: http://localhost:8091
- Meta Frontend: http://localhost:8092
- Transfer Frontend: http://localhost:8093
- Orchestrator Frontend: http://localhost:8094
- Develop Frontend: http://localhost:8095

**后端 API**:
- Gateway API: http://localhost:8000
- System Backend: http://localhost:8080
- Manager Backend: http://localhost:8081
- Meta Backend: http://localhost:8082
- Transfer Backend: http://localhost:8083
- Orchestrator Backend: http://localhost:8084
- Develop Backend: http://localhost:8085

## 架构说明

### Nginx 统一网关路由规则

```nginx
/               → Portal (统一入口)
/api/*          → Gateway:8000 (后端 API)
/system/*       → System Frontend:8090
/manager/*      → Manager Frontend:8091
/meta/*         → Meta Frontend:8092
/transfer/*     → Transfer Frontend:8093
/health         → Nginx 健康检查
```

### Portal 设计

Portal 作为微前端架构的统一入口：

- **认证**: 统一通过 System Backend 认证，Token 存储在 localStorage
- **导航**: 左侧菜单统一管理所有模块入口
- **集成**: 通过 iframe 加载各模块前端，保持模块独立性
- **路由**: Portal 的 nginx.conf 将 `/api` 请求代理到 Gateway

### 服务依赖关系

```
Infrastructure (PostgreSQL, Redis, MinIO)
  ↓
System Backend (配置中心 + 认证中心)
  ↓
Manager/Meta/Transfer/Orchestrator/Develop Backends (业务后端)
  ↓
Gateway (API 路由)
  ↓
Frontend Services (各模块前端)
  ↓
Portal + Nginx (统一入口)
```

## 服务管理

### 查看服务状态

```bash
# 基础设施服务
docker compose -f docker-compose.infra.yml ps

# 应用服务
docker compose -f docker-compose.yml ps

# 查看所有容器
docker ps
```

### 查看日志

```bash
# 查看基础设施日志
docker compose -f docker-compose.infra.yml logs -f [service-name]

# 查看应用服务日志
docker compose -f docker-compose.yml logs -f [service-name]

# 示例: 查看 Portal 日志
docker compose -f docker-compose.yml logs -f portal

# 示例: 查看 Nginx 日志
docker compose -f docker-compose.yml logs -f nginx
```

### 重启服务

```bash
# 重启单个服务
docker compose -f docker-compose.yml restart [service-name]

# 示例: 重启 Portal
docker compose -f docker-compose.yml restart portal

# 示例: 重启 Nginx
docker compose -f docker-compose.yml restart nginx

# 重启所有应用服务
docker compose -f docker-compose.yml restart
```

### 停止服务

```bash
# 停止应用服务
docker compose -f docker-compose.yml down

# 停止基础设施
docker compose -f docker-compose.infra.yml down

# 停止所有服务
docker compose -f docker-compose.infra.yml down && docker compose -f docker-compose.yml down
```

## 健康检查

使用提供的健康检查脚本：

```bash
bash scripts/prod/health-check.sh
```

或手动测试关键服务：

```bash
# Portal (通过 Nginx)
curl http://localhost:80

# Portal (独立访问)
curl http://localhost:5170

# Nginx 健康检查
curl http://localhost:80/health

# Gateway API
curl http://localhost:8000/health

# System Backend
curl http://localhost:8080/health
```

## 常见问题

### 1. Portal 显示空白页面

**检查步骤**:
```bash
# 1. 检查 Portal 容器是否运行
docker ps | grep portal

# 2. 查看 Portal 日志
docker compose -f docker-compose.yml logs portal

# 3. 检查 Nginx 配置是否正确
docker exec nginx cat /etc/nginx/nginx.conf

# 4. 重启 Portal 和 Nginx
docker compose -f docker-compose.yml restart portal nginx
```

### 2. Nginx 显示 502 Bad Gateway

**原因**: 后端服务未启动或未就绪

**解决方法**:
```bash
# 1. 检查 Gateway 是否运行
curl http://localhost:8000/health

# 2. 检查 Portal 是否运行
curl http://localhost:5170

# 3. 查看 Nginx 日志
docker compose -f docker-compose.yml logs nginx

# 4. 重新启动后端服务
./scripts/prod/start.sh
```

### 3. 前端无法访问后端 API

**检查步骤**:
```bash
# 1. 测试 Gateway 是否可访问
curl http://localhost:8000/health

# 2. 测试 Nginx API 代理
curl http://localhost:80/api/system/health

# 3. 检查网络连通性
docker network inspect addp-network
```

### 4. 部分服务未启动

**检查脚本是否正确执行**:
```bash
# 重新执行完整部署
./scripts/prod/start.sh

# 手动启动缺失的服务
docker compose -f docker-compose.yml up -d [service-name]
```

## 镜像管理

### 查看已构建的镜像

```bash
docker images | grep addp
```

### 重新构建镜像

```bash
# 构建所有镜像（从项目根目录）
./scripts/deploy/1-build-images.sh

# 或单独构建某个服务
docker build -t localhost:5001/addp-portal:latest portal/frontend/
docker build -t localhost:5001/addp-nginx:latest nginx/
```

## 性能优化建议

1. **资源限制**: 在 `docker-compose.yml` 中为容器设置资源限制
2. **Nginx 缓存**: 配置静态资源缓存提升响应速度
3. **数据库连接池**: 调整后端服务的数据库连接池大小
4. **Worker 副本数**: 根据负载调整 Worker 服务副本数量

## 安全建议

1. **修改默认密码**: 生产环境必须修改 `.env` 中的所有默认密码
2. **启用 HTTPS**: 配置 SSL 证书，使用 HTTPS 访问
3. **防火墙配置**: 仅开放必要的端口（80/443）
4. **定期备份**: 配置数据库和 MinIO 的定期备份
5. **禁用默认账户**: 生产环境禁用 `ENABLE_DEFAULT_TENANT=true`

## 相关文档

- [CLAUDE.md](../CLAUDE.md) - 完整架构文档
- [nginx/nginx.conf](../nginx/nginx.conf) - Nginx 路由配置
- [portal/frontend/README.md](../portal/frontend/README.md) - Portal 开发文档
- [scripts/prod/start.sh](../scripts/prod/start.sh) - 部署脚本源码
