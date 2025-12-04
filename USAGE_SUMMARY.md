# ADDP 使用总结

## ✅ 已完成的工作

### 1. 完整的部署系统
- ✅ 多架构镜像构建脚本
- ✅ 部署打包脚本
- ✅ 服务器初始化脚本
- ✅ 一键部署脚本
- ✅ PostgreSQL 自定义镜像（含超级管理员）

### 2. 生产环境启动脚本
- ✅ `scripts/prod/start.sh` - 一键启动生产环境
- ✅ `scripts/prod/stop.sh` - 停止生产环境
- ✅ 自动生成安全密钥
- ✅ 自动检查端口冲突
- ✅ 健康检查和状态显示

### 3. 完整文档
- ✅ [START_HERE.md](START_HERE.md) - 最简使用说明
- ✅ [QUICK_START.md](QUICK_START.md) - 快速启动指南
- ✅ [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) - 完整部署文档
- ✅ [scripts/deploy/README.md](scripts/deploy/README.md) - 部署脚本说明

---

## 🚀 如何使用

### 本地开发/测试环境

#### 最简单的方式：

```bash
# 1. 启动 registry（只需要一次）
docker run -d -p 5001:5000 --restart=always --name registry registry:2

# 2. 启动 ADDP
./scripts/prod/start.sh

# 3. 访问
open http://localhost:8090
```

#### 使用 Makefile：

```bash
make prod-start    # 启动
make prod-stop     # 停止
make prod-restart  # 重启
make prod-logs     # 查看日志
make prod-status   # 查看状态
```

---

### 生产服务器部署

#### 方法 1: 一键部署（推荐）

```bash
./scripts/deploy/deploy-all.sh \
  --server user@production-server \
  --registry localhost:5001
```

#### 方法 2: 分步部署

```bash
# Step 1: 构建镜像（开发机）
./scripts/deploy/1-build-images.sh --registry localhost:5001

# Step 2: 打包并传输（开发机）
./scripts/deploy/2-package-deploy.sh \
  --server user@server \
  --registry localhost:5001

# Step 3: 服务器上运行（服务器）
ssh user@server
cd ~/addp
./scripts/3-server-setup.sh
```

---

## 📁 重要文件说明

### 启动脚本
- `scripts/prod/start.sh` - **生产环境启动脚本（本地使用）**
- `scripts/prod/stop.sh` - 生产环境停止脚本

### 部署脚本
- `scripts/deploy/deploy-all.sh` - 一键部署到服务器
- `scripts/deploy/1-build-images.sh` - 多架构镜像构建
- `scripts/deploy/2-package-deploy.sh` - 打包部署文件
- `scripts/deploy/3-server-setup.sh` - 服务器初始化

### 配置文件
- `.env.prod.example` - 生产环境配置模板
- `.env.prod` - 生产环境配置（自动生成）
- `docker-compose.prod.yml` - Docker Compose 生产配置

### 数据库初始化
- `scripts/postgres/Dockerfile` - PostgreSQL 自定义镜像
- `scripts/postgres/init-db.sql` - 数据库初始化脚本（含超级管理员）

---

## 🔑 默认凭证

### 超级管理员
- Username: `SuperAdmin`
- Password: `20251001#SuperAdmin`
- Email: `admin@addp.local`

**⚠️ 重要**: 首次登录后立即修改密码！

### 其他服务
密钥自动生成并保存在 `.env.prod` 中

---

## 📊 服务访问地址

| 服务 | 地址 | 说明 |
|------|------|------|
| System Frontend | http://localhost:8090 | 主界面 |
| System Backend | http://localhost:8080 | API |
| MinIO Console | http://localhost:9001 | 对象存储 |
| PostgreSQL | localhost:5432 | 数据库 |
| Redis | localhost:6379 | 缓存 |

---

## 🛠️ 常用命令

### 启动和停止
```bash
./scripts/prod/start.sh    # 启动
./scripts/prod/stop.sh     # 停止
```

### 查看状态
```bash
docker compose -f docker-compose.prod.yml ps
```

### 查看日志
```bash
# 所有服务
docker compose -f docker-compose.prod.yml logs -f

# 特定服务
docker compose -f docker-compose.prod.yml logs -f system-backend
```

### 重启服务
```bash
docker compose -f docker-compose.prod.yml restart
docker compose -f docker-compose.prod.yml restart system-backend  # 重启特定服务
```

### 进入容器
```bash
docker compose -f docker-compose.prod.yml exec system-backend sh
docker compose -f docker-compose.prod.yml exec postgres psql -U addp -d addp
```

---

## 🔍 故障排查

### 启动脚本自动处理的问题
- ✅ 自动创建 `.env.prod`
- ✅ 自动生成安全密钥（base64 格式）
- ✅ 检查 registry 可访问性
- ✅ 检查并清理端口冲突
- ✅ 执行健康检查

### 手动排查

#### 1. Registry 无法访问
```bash
docker ps | grep registry
docker run -d -p 5001:5000 --restart=always --name registry registry:2
```

#### 2. 端口冲突
```bash
lsof -i :8080    # 查看占用端口的进程
lsof -ti:8080 | xargs kill -9    # 杀掉进程
```

#### 3. 服务无法启动
```bash
docker compose -f docker-compose.prod.yml logs service-name
```

---

## 📝 开发vs生产

### 开发环境
```bash
make dev-start     # 启动开发环境
make dev-stop      # 停止开发环境
```

### 生产环境
```bash
./scripts/prod/start.sh    # 或 make prod-start
./scripts/prod/stop.sh     # 或 make prod-stop
```

**区别**:
- 开发环境: 代码热重载，直接运行 Go 代码
- 生产环境: Docker 容器化，使用编译好的镜像

---

## ✅ 验证清单

启动后验证：

```bash
# 1. 健康检查
curl http://localhost:8080/health

# 2. 登录测试
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"SuperAdmin","password":"20251001#SuperAdmin"}'

# 3. 浏览器访问
open http://localhost:8090
```

---

## 📚 文档索引

| 文档 | 用途 |
|------|------|
| [START_HERE.md](START_HERE.md) | 最简使用说明 |
| [QUICK_START.md](QUICK_START.md) | 快速启动详细指南 |
| [DEPLOYMENT.md](docs/DEPLOYMENT.md) | 完整部署文档 |
| [CLAUDE.md](CLAUDE.md) | 架构和开发指南 |
| [deploy/README.md](scripts/deploy/README.md) | 部署脚本说明 |

---

## 🎯 总结

**现在您可以**:

1. ✅ 一条命令启动本地环境: `./scripts/prod/start.sh`
2. ✅ 一条命令部署到服务器: `./scripts/deploy/deploy-all.sh --server user@host`
3. ✅ 自动处理所有配置和密钥生成
4. ✅ 自动检查和修复常见问题
5. ✅ 获得清晰的成功/失败提示

**不再需要**:
- ❌ 手动创建 `.env.prod`
- ❌ 手动生成密钥
- ❌ 手动指定 `--env-file`
- ❌ 手动清理端口
- ❌ 记住复杂的命令

---

**版本**: v0.0.6
**最后更新**: 2025-10-31
