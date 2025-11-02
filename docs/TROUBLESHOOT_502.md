# 502 Bad Gateway 和页面空白问题排查指南

## 问题症状

1. **从本机访问**: `http://192.168.1.182:8000/` 返回 `502 Bad Gateway`
2. **从服务器本地访问**: 能登录,但登录后所有页面空白

## 快速诊断步骤

### 步骤 1: SSH 到服务器并进入部署目录

```bash
ssh pampa@192.168.1.182
cd ~/addp
```

### 步骤 2: 检查所有服务状态

```bash
docker compose -f docker-compose.prod.yml ps
```

**期望输出**: 所有服务状态应为 `Up` 或 `Up (healthy)`

**常见问题**:
- 服务状态为 `Exit` → 服务已崩溃
- 服务状态为 `Up (unhealthy)` → 服务启动了但健康检查失败

### 步骤 3: 运行自动诊断脚本

```bash
bash scripts/diagnose-502.sh
```

这会检查:
- ✅ 所有服务状态
- ✅ 不健康的服务列表
- ✅ Nginx 错误日志
- ✅ Gateway 日志
- ✅ System Backend 日志
- ✅ 内部服务连接测试
- ✅ 端口监听状态

## 常见问题和解决方案

### 问题 1: Nginx 报 502 - Gateway 不可用

**症状**:
```
nginx: connect() failed (111: Connection refused) while connecting to upstream
```

**原因**: Nginx 无法连接到 Gateway 服务

**排查**:
```bash
# 检查 Gateway 是否运行
docker compose -f docker-compose.prod.yml ps gateway

# 查看 Gateway 日志
docker compose -f docker-compose.prod.yml logs --tail=50 gateway
```

**修复**:
```bash
# 重启 Gateway
docker compose -f docker-compose.prod.yml restart gateway

# 如果还是失败,重新拉取镜像并重启
docker compose -f docker-compose.prod.yml pull gateway
docker compose -f docker-compose.prod.yml up -d gateway
```

### 问题 2: 页面空白 - 前端服务未加载

**症状**: 能看到登录页面,登录成功后页面空白,浏览器 Console 有 404 错误

**原因**: Portal 或前端服务未正确启动

**排查**:
```bash
# 检查前端服务状态
docker compose -f docker-compose.prod.yml ps portal system-frontend manager-frontend

# 查看 Portal 日志
docker compose -f docker-compose.prod.yml logs --tail=50 portal
```

**修复**:
```bash
# 重启所有前端服务
docker compose -f docker-compose.prod.yml restart portal system-frontend manager-frontend meta-frontend transfer-frontend
```

### 问题 3: API 调用失败 - 后端服务不可用

**症状**: 浏览器 Console 显示 `/api/...` 请求失败 (500, 502, 503)

**原因**: System/Manager/Meta Backend 服务未启动或崩溃

**排查**:
```bash
# 检查所有后端服务
docker compose -f docker-compose.prod.yml ps system-backend manager-backend meta-backend

# 查看 System Backend 日志 (最重要,负责认证)
docker compose -f docker-compose.prod.yml logs --tail=100 system-backend
```

**修复**:
```bash
# 重启所有后端服务
docker compose -f docker-compose.prod.yml restart system-backend manager-backend meta-backend gateway
```

### 问题 4: 基础设施服务不健康

**症状**: Redis/MinIO/Elasticsearch 显示 `unhealthy`

**排查**:
```bash
# 检查基础设施服务
docker compose -f docker-compose.prod.yml ps redis minio-system elasticsearch

# 查看日志
docker compose -f docker-compose.prod.yml logs redis
docker compose -f docker-compose.prod.yml logs minio-system
docker compose -f docker-compose.prod.yml logs elasticsearch
```

**修复**:
```bash
# 重启基础设施服务
docker compose -f docker-compose.prod.yml restart redis minio-system elasticsearch

# 等待服务启动 (约 30-60 秒)
sleep 30

# 重启依赖这些服务的后端
docker compose -f docker-compose.prod.yml restart system-backend manager-backend meta-backend
```

### 问题 5: Registry 不可访问

**症状**: 服务无法从 `localhost:5001` 拉取镜像

**排查**:
```bash
# 检查 Registry 是否运行
docker ps | grep registry

# 测试 Registry 连接
curl http://localhost:5001/v2/_catalog
```

**修复**:
```bash
# 启动 Registry (如果未运行)
docker run -d -p 5001:5000 --name registry --restart=always registry:2
```

## 完整重启流程

如果以上方法都不奏效,执行完整重启:

```bash
# 1. 停止所有服务
docker compose -f docker-compose.prod.yml down

# 2. 检查是否有残留容器
docker ps -a | grep addp

# 3. 清理残留容器 (如果有)
docker compose -f docker-compose.prod.yml rm -f

# 4. 重新启动所有服务
docker compose -f docker-compose.prod.yml up -d

# 5. 查看启动日志
docker compose -f docker-compose.prod.yml logs -f
```

## 日志查看命令

### 实时查看所有日志
```bash
docker compose -f docker-compose.prod.yml logs -f
```

### 查看特定服务日志
```bash
# Nginx (负责 8000 端口)
docker compose -f docker-compose.prod.yml logs -f nginx

# Gateway (API 路由)
docker compose -f docker-compose.prod.yml logs -f gateway

# System Backend (认证和用户管理)
docker compose -f docker-compose.prod.yml logs -f system-backend

# Portal (统一入口)
docker compose -f docker-compose.prod.yml logs -f portal
```

### 查看最近 N 行日志
```bash
docker compose -f docker-compose.prod.yml logs --tail=100 nginx
docker compose -f docker-compose.prod.yml logs --tail=100 gateway
```

## 网络连接测试

### 测试从 Nginx 到 Gateway
```bash
docker compose -f docker-compose.prod.yml exec nginx wget -qO- http://gateway:8000/health
```

### 测试从 Gateway 到 System Backend
```bash
docker compose -f docker-compose.prod.yml exec gateway wget -qO- http://system-backend:8080/health
```

### 测试从宿主机到容器
```bash
# 测试 Nginx (应该返回 502 或页面内容)
curl -v http://localhost:8000/

# 测试 Gateway (如果映射了端口)
curl http://localhost:8000/health
```

## 架构检查清单

确保以下服务链路正常:

```
浏览器 (192.168.1.182:8000)
  ↓
Nginx (容器内 80 → 宿主机 8000)
  ↓
Gateway (容器内 8000)
  ↓
System Backend (容器内 8080) - 认证
Manager Backend (容器内 8081) - 数据管理
Meta Backend (容器内 8082) - 元数据
  ↓
Redis (容器内 6379) - 缓存
PostgreSQL (容器内 5432) - 数据库
MinIO (容器内 9000) - 对象存储
```

检查每一层:

1. ✅ **Nginx → Gateway**: `docker compose logs nginx | grep "upstream"`
2. ✅ **Gateway → Backends**: `docker compose logs gateway | grep "proxy"`
3. ✅ **Backends → Infrastructure**: `docker compose logs system-backend | grep "redis\|postgres"`

## 配置文件检查

### 检查 .env.prod
```bash
cat .env.prod | grep -v "^#" | grep -v "^$"
```

**关键配置**:
- `REGISTRY=localhost:5001`
- `JWT_SECRET` 应该有值
- `POSTGRES_PASSWORD` 应该有值
- `REDIS_PASSWORD` 应该有值

### 检查 docker-compose.prod.yml
```bash
# 检查 Nginx 端口映射 (应该是 8000:80)
grep -A 3 "nginx:" docker-compose.prod.yml | grep ports

# 检查所有服务的 image 配置
grep "image:" docker-compose.prod.yml
```

## 浏览器调试

### Chrome DevTools Console 常见错误

1. **ERR_HTTP_RESPONSE_CODE_FAILURE 502**
   - 原因: Nginx → Gateway 连接失败
   - 修复: 重启 Gateway 和 Nginx

2. **404 Not Found (静态资源)**
   - 原因: Portal/Frontend 服务未启动
   - 修复: 重启前端服务

3. **401 Unauthorized (API 调用)**
   - 原因: JWT token 无效或 System Backend 不可用
   - 修复: 重启 System Backend,清除浏览器缓存

4. **500 Internal Server Error (API 调用)**
   - 原因: 后端服务崩溃或数据库连接失败
   - 修复: 查看后端日志,重启相关服务

### Network Tab 检查

打开浏览器 DevTools → Network:

1. **检查 `/` 请求** (首页)
   - 状态码应为 `200 OK`
   - 如果是 `502`,说明 Nginx → Gateway 失败

2. **检查 `/api/auth/login` 请求** (登录)
   - 状态码应为 `200 OK`
   - 如果是 `502`,说明 Gateway → System Backend 失败

3. **检查静态资源请求** (`.js`, `.css`)
   - 状态码应为 `200 OK`
   - 如果是 `404`,说明前端服务未正确部署

## 快速修复命令汇总

```bash
# SSH 到服务器
ssh pampa@192.168.1.182

# 进入部署目录
cd ~/addp

# 查看服务状态
docker compose -f docker-compose.prod.yml ps

# 运行诊断
bash scripts/diagnose-502.sh

# 重启所有服务 (最快的修复方法)
docker compose -f docker-compose.prod.yml restart

# 或者完全重启
docker compose -f docker-compose.prod.yml down
docker compose -f docker-compose.prod.yml up -d

# 查看实时日志
docker compose -f docker-compose.prod.yml logs -f | grep -E "(error|Error|ERROR|fail|Fail|FAIL)"
```

## 预防措施

### 健康检查脚本 (定期运行)

创建 `~/addp/scripts/health-check.sh`:

```bash
#!/bin/bash
echo "=== ADDP Health Check ==="
echo "Time: $(date)"
echo ""

# 检查服务状态
echo "Services status:"
docker compose -f ~/addp/docker-compose.prod.yml ps

# 检查不健康的服务
UNHEALTHY=$(docker compose -f ~/addp/docker-compose.prod.yml ps --filter "health=unhealthy" -q | wc -l)
if [ "$UNHEALTHY" -gt 0 ]; then
    echo "WARNING: $UNHEALTHY unhealthy services detected!"
    docker compose -f ~/addp/docker-compose.prod.yml ps --filter "health=unhealthy"
fi

# 测试 HTTP 响应
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8000/)
echo "HTTP Response Code: $HTTP_CODE"
if [ "$HTTP_CODE" != "200" ]; then
    echo "WARNING: HTTP check failed!"
fi
```

### 设置 Cron 自动检查 (可选)

```bash
# 每 5 分钟检查一次
*/5 * * * * /bin/bash ~/addp/scripts/health-check.sh >> ~/addp/logs/health-check.log 2>&1
```

## 联系支持

如果以上方法都无法解决问题,请收集以下信息:

```bash
# 1. 服务状态
docker compose -f docker-compose.prod.yml ps > ~/addp-debug-status.txt

# 2. 所有日志
docker compose -f docker-compose.prod.yml logs --tail=200 > ~/addp-debug-logs.txt

# 3. 系统信息
uname -a > ~/addp-debug-system.txt
docker version >> ~/addp-debug-system.txt
docker compose version >> ~/addp-debug-system.txt

# 打包发送
tar -czf ~/addp-debug-$(date +%Y%m%d-%H%M%S).tar.gz ~/addp-debug-*.txt
```
