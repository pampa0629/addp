# 部署到服务器完整指南

## 🎯 当前状态

### ✅ 开发机已完成
1. ✅ 所有 ADDP 应用镜像已推送（7个）
2. ✅ 所有基础设施镜像已推送（4个）
3. ✅ PostgreSQL 使用自定义镜像（包含初始化脚本，无需文件挂载）
4. ✅ docker-compose.prod.yml 已更新
5. ✅ 所有推送脚本已修复

### 📦 Registry 镜像清单（12个）

**ADDP 应用镜像（7个）**:
- addp-system-backend
- addp-system-frontend
- addp-manager-backend
- addp-manager-frontend
- addp-meta-backend
- addp-gateway
- addp-portal

**基础设施镜像（4个）**:
- addp-infra-postgres-init:15-alpine ⭐ **新版本，含初始化脚本**
- addp-infra-redis:7-alpine
- addp-infra-minio:latest
- addp-infra-elasticsearch:8.11.0

---

## 🚀 部署步骤

### 步骤 1: 传输配置文件到服务器

选择一种方式：

#### 方式 A: 使用一键部署

```bash
# 在开发机执行（自动传输所需文件并在服务器启动）
./scripts/deploy/deploy-all.sh --server pampa@192.168.31.174 --registry 192.168.31.238:5001
```

#### 方式 B: 使用 U 盘

1. 复制以下文件到 U 盘:
   - `docker-compose.prod.yml`
   - `business/docker-compose.prod.yml`
   - 使用 `scripts/deploy/deploy-all.sh` 无需单独传输部署脚本

2. 在服务器上：
```bash
# 假设 U 盘挂载在 /Volumes/USB
cp /Volumes/USB/docker-compose.prod.yml ~/addp/
cp /Volumes/USB/docker-compose.prod.yml ~/addp/business/
# 使用一键部署脚本无需单独传输部署脚本
```

#### 方式 C: 手动创建（如果网络不通）

在服务器上直接编辑文件，参考开发机的内容。

---

### 步骤 2: 在服务器上部署

```bash
# 1. SSH 登录服务器
ssh pampa@192.168.31.174

# 2. 进入部署目录（使用用户目录，避免权限问题）
cd ~/addp

# 3. （若未使用一键部署）可以按以下命令手动执行
#    - 构建/推送镜像（在开发机）
#    - 服务器上 docker compose 启动
```

脚本会自动：
1. ✅ 配置 Docker 信任私有 Registry
2. ✅ 拉取所有镜像（应用 + 基础设施）
3. ✅ 启动所有服务

---

### 步骤 3: 验证部署

```bash
# 查看所有容器状态
docker-compose -f docker-compose.prod.yml ps

# 应该看到所有服务都是 Up 状态:
# NAME                     STATE
# addp-postgres            Up (healthy)
# addp-redis               Up (healthy)
# addp-minio-system        Up
# addp-elasticsearch       Up
# addp-system-backend      Up (healthy)
# addp-gateway             Up (healthy)
# addp-portal              Up
# ...

# 查看日志
docker-compose -f docker-compose.prod.yml logs -f

# 测试访问
curl http://localhost:8080/health  # System backend
curl http://localhost:8000/health  # Gateway
curl http://localhost:5170         # Portal (在浏览器中打开)
```

---

## 🔧 重要改进说明

### PostgreSQL 初始化脚本改进

**之前的问题**:
```yaml
volumes:
  - ./scripts/init-db.sql:/docker-entrypoint-initdb.d/init-db.sql
```
❌ macOS Docker Desktop 默认不共享 `/opt` 目录
❌ 需要手动配置文件共享
❌ 部署到不同路径需要修改配置

**现在的解决方案**:
```yaml
image: ${REGISTRY}/addp-infra-postgres-init:15-alpine
# 无需挂载文件！
```
✅ 初始化脚本已嵌入镜像
✅ 无需文件共享配置
✅ 任意路径部署
✅ 更符合容器化最佳实践

---

## 🎯 关键配置参数

### 环境变量

在服务器的 `~/addp/.env` 文件中配置：

```bash
# Registry（从开发机拉取）
REGISTRY=192.168.31.238:5001

# 安全密钥（生产环境必须修改）
JWT_SECRET=<生成随机密钥>
ENCRYPTION_KEY=<生成随机密钥>

# 数据库
POSTGRES_PASSWORD=<强密码>
POSTGRES_USER=addp
POSTGRES_DB=addp

# Redis
REDIS_PASSWORD=<强密码>

# MinIO
MINIO_SYSTEM_ROOT_PASSWORD=<强密码>

# 生成随机密钥命令:
# openssl rand -base64 32
```

### Business 基础设施

在服务器的 `~/addp/business/.env` 文件中配置：

```bash
REGISTRY=192.168.31.238:5001
POSTGRES_PASSWORD=<强密码>
MINIO_ROOT_PASSWORD=<强密码>
```

---

## ⚠️ 常见问题排查

### 问题 1: 无法拉取镜像

**错误**: `Error response from daemon: Get "https://192.168.31.238:5001/v2/": http: server gave HTTP response to HTTPS client`

**解决**: Docker 未配置信任私有 Registry

```bash
# macOS - 通过 Docker Desktop GUI 配置
1. Docker Desktop → Settings → Docker Engine
2. 添加: "insecure-registries": ["192.168.31.238:5001"]
3. Apply & Restart

# 验证
docker info | grep "192.168.31.238:5001"
```

### 问题 2: 网络连接问题

**错误**: `dial tcp 192.168.31.238:5001: connect: connection refused`

**检查**:
```bash
# 在服务器上测试
ping 192.168.31.238
telnet 192.168.31.238 5001
curl http://192.168.31.238:5001/v2/_catalog

# 在开发机上检查 Registry 是否运行
docker ps | grep registry
```

### 问题 3: 端口冲突

**错误**: `port is already allocated`

**解决**: 修改 docker-compose.prod.yml 中的端口映射

```yaml
ports:
  - "15432:5432"  # 如果 5432 被占用
```

### 问题 4: 内存不足

**错误**: Elasticsearch 启动失败

**解决**: 调整 ES 内存限制

```yaml
elasticsearch:
  environment:
    - "ES_JAVA_OPTS=-Xms512m -Xmx512m"  # 降低内存使用
```

---

## 📊 服务访问地址

部署成功后：

| 服务 | 地址 | 说明 |
|------|------|------|
| **Portal** | http://192.168.31.174:5170 | 统一入口（推荐） |
| Gateway | http://192.168.31.174:8000 | API 网关 |
| System Backend | http://192.168.31.174:8080 | 系统后端 API |
| MinIO Console | http://192.168.31.174:9001 | 文件管理 |
| Elasticsearch | http://192.168.31.174:9200 | 搜索引擎 |

---

## 🔄 更新部署

当代码更新后：

```bash
# 在开发机重新构建和推送
./scripts/deploy/1-build-images-multiarch.sh --registry 5001

# 在服务器重新拉取和部署
ssh pampa@192.168.31.174
cd ~/addp
REGISTRY=192.168.31.238:5001 docker-compose -f docker-compose.prod.yml pull
docker-compose -f docker-compose.prod.yml up -d
```

---

## 📝 快速参考

| 项目 | 值 |
|------|-----|
| 开发机 IP | 192.168.31.238 |
| 服务器 IP | 192.168.31.174 |
| Registry 端口 | 5001 |
| 部署目录 | ~/addp |
| 总镜像数 | 12 个 |

---

## ✅ 检查清单

部署前确认：

- [ ] 开发机 Registry 正在运行 (`docker ps | grep registry`)
- [ ] 所有镜像已推送 (`curl http://localhost:5001/v2/_catalog`)
- [ ] 服务器可以访问开发机 (`ping 192.168.31.238`)
- [ ] 服务器 Docker 已配置信任 Registry
- [ ] 配置文件已传输到服务器
- [ ] .env 文件已配置密钥和密码

部署后确认：

- [ ] 所有容器状态为 Up (`docker-compose ps`)
- [ ] 健康检查通过 (`docker-compose ps` 显示 healthy)
- [ ] Portal 可访问 (http://192.168.31.174:5170)
- [ ] 无错误日志 (`docker-compose logs`)

---

现在可以开始部署了！🚀
