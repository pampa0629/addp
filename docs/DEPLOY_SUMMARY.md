# 部署问题总结与解决方案

## 遇到的问题

### 1. 端口 5000 被占用
- **问题**: macOS AirPlay Receiver 占用端口 5000
- **解决**: 使用端口 5001，更新所有脚本支持自定义端口

### 2. 跨平台架构不兼容
- **问题**: ARM Mac (M1/M2/M3) 构建的镜像无法在 Intel x86 服务器运行
- **解决**: 使用 Docker Buildx 多平台构建 (linux/amd64, linux/arm64)

### 3. macOS chown 兼容性问题
- **问题**: `chown user:user` 在 macOS 上报错 (用户组不是用户名)
- **解决**: 脚本检测操作系统，macOS 使用 `chown user`，Linux 使用 `chown user:user`

### 4. SSH 连接被拒绝
- **问题**: macOS 服务器默认未启用 SSH
- **解决**: System Settings → Sharing → Remote Login

### 5. Registry 500 错误
- **问题**: Docker 不信任 insecure registry
- **解决**: 配置 Docker Desktop → Settings → Docker Engine → `insecure-registries`

### 6. 多架构镜像构建网络问题
- **问题**: Docker Buildx container-driver 网络隔离，无法访问 Docker Hub 和 Go proxy
- **表现**:
  - Docker Hub IPv6 超时
  - goproxy.cn 超时
  - proxy.golang.org 超时
- **根本原因**: buildx container 网络配置问题，尝试多种解决方案均失败

### 7. Go 版本不匹配
- **问题**: go.mod 要求 Go 1.24，但 Dockerfile 使用 golang:1.23-alpine
- **解决**: 更新所有 Dockerfile 使用 golang:1.24-alpine

### 8. Portal Dockerfile 缺失
- **问题**: portal/frontend/ 目录缺少 Dockerfile 和 nginx.conf
- **解决**: 创建 Portal 的 Dockerfile 和 nginx.conf

---

## 最终解决方案

鉴于多架构构建的持续网络问题，推荐使用**服务器本地构建**的方式：

### 方案优势

✅ **无需镜像仓库** - 直接在服务器构建
✅ **无跨平台问题** - 服务器自动构建本地架构
✅ **无网络隔离** - 直接使用服务器网络
✅ **简化部署** - 一个脚本完成所有操作

### 部署步骤

#### 快速部署（推荐）

```bash
# 在开发机执行
./scripts/prod/deploy.sh v1.0.0 --registry localhost:5001
```

一键完成：
1. 编译多架构二进制
2. 构建并推送镜像
3. 部署到生产环境
4. 启动所有服务

#### 手动部署

```bash
# 1. 传输代码
rsync -avz --exclude 'node_modules' --exclude '.git' \
  ./ pampa@192.168.1.182:/opt/addp/

# 2. SSH 登录服务器
ssh pampa@192.168.1.182

# 3. 部署 Business 基础设施
cd /opt/addp/business
cp .env.prod.example .env
vim .env  # 配置密码
docker-compose -f docker-compose.yml up -d

# 4. 构建并部署 ADDP（服务器本地构建）
cd /opt/addp
cp .env.prod.example .env.prod
vim .env.prod  # 配置密钥和密码

# 可选：预编译二进制（提升构建速度）
./scripts/build/compile.sh --arch amd64

# 构建并推送镜像到本地 registry（localhost:5001）
./scripts/build/build-images.sh --registry localhost:5001

# 启动
docker compose -f docker-compose.yml --env-file .env.prod up -d
```

---

## 文件清单

### 新增脚本

1. **scripts/setup/local-registry.sh** - 本地 Registry 设置（支持自定义端口）
2. **scripts/build/compile.sh** - 本地预编译二进制（可选，加速构建）
3. **scripts/build/build-images.sh** - 本地构建并推送镜像（单架构或多架构）
4. **scripts/prod/deploy.sh** - 一键部署脚本（编译 → 构建 → 部署）
5. **scripts/deploy/2-package-deploy.sh** - 产物打包与可选传输
6. **scripts/deploy/3-server-setup.sh** - 服务器初始化与启动
7. **scripts/deploy/deploy-all.sh** - 一键部署（构建+传输+启动）

### Business 基础设施

1. **business/docker-compose.yml** - Business 独立部署配置
2. **business/.env.prod.example** - Business 环境变量模板
3. **business/scripts/deploy-business.sh** - Business 部署脚本（macOS 兼容）
4. **business/scripts/prepare-for-deploy.sh** - Business 打包脚本
5. **business/DEPLOY.md** - Business 部署文档

### 文档

1. **docs/DEPLOY_WITH_LOCAL_REGISTRY.md** - 使用本地 Registry 部署（完整指南）
2. **docs/DEPLOY_SERVER_DIRECT.md** - 服务器直接构建部署（推荐）
3. **docs/TROUBLESHOOT_PORT_5000.md** - 端口 5000 冲突排查
4. **docs/TROUBLESHOOT_SSH.md** - SSH 连接问题排查
5. **docs/TROUBLESHOOT_REGISTRY_500.md** - Registry 500 错误排查
6. **docs/FIX_MACOS_SERVER_CHOWN.md** - macOS chown 修复
7. **docs/CROSS_PLATFORM_DEPLOYMENT.md** - 跨平台部署说明
8. **docs/MULTI_ARCH_IMAGES.md** - 多架构镜像原理
9. **docs/MACOS_DEPLOYMENT_FIX.md** - macOS 部署兼容性
10. **docs/DEPLOY_SUMMARY.md** - 本文档

### 配置修改

1. **system/backend/Dockerfile** - 更新 Go 版本到 1.24
2. **manager/backend/Dockerfile** - 更新 Go 版本到 1.24
3. **meta/backend/Dockerfile** - 更新 Go 版本到 1.24
4. **gateway/Dockerfile** - 更新 Go 版本到 1.24
5. **portal/frontend/Dockerfile** - 新建 Portal Dockerfile
6. **portal/frontend/nginx.conf** - 新建 Portal nginx 配置
7. **system/backend/go.mod** - 降级 Go 版本到 1.23
8. **manager/backend/go.mod** - 降级 Go 版本到 1.23
9. **meta/backend/go.mod** - 降级 Go 版本到 1.23
10. **gateway/go.mod** - 降级 Go 版本到 1.23
11. **common/go.mod** - 降级 Go 版本到 1.23

### docker-compose 修改

1. **docker-compose.yml** - 移除 obsolete `version: '3.8'`
2. **business/docker-compose.yml** - 移除 obsolete `version: '3.8'`

---

## 推荐部署流程

### 开发环境
```bash
# 启动本地开发
make dev-start
```

### 生产环境（局域网服务器）
```bash
# 一键部署到服务器
./scripts/deploy/deploy-all.sh --server pampa@192.168.1.182 --registry localhost:5001
```

### 生产环境（云服务器，网络良好）
```bash
# 使用多架构镜像仓库（需要网络通畅）
./scripts/setup/local-registry.sh 5001
./scripts/deploy/1-build-images-multiarch.sh --registry 开发机IP:5001

# 在开发机触发一键部署
./scripts/deploy/deploy-all.sh --server user@server-ip --registry 开发机IP:5001 --skip-build
```

---

## 经验教训

1. **跨平台构建复杂** - 需要 QEMU 模拟，网络依赖多
2. **Docker Buildx 网络隔离** - container-driver 经常遇到网络问题
3. **macOS 兼容性** - 用户组、chown 语法与 Linux 不同
4. **Go 版本管理** - Dockerfile、go.mod、依赖三者版本需一致
5. **本地构建更可靠** - 对于局域网部署，服务器本地构建最简单

---

## 下一步优化

1. **CI/CD 集成** - GitHub Actions 自动构建多架构镜像
2. **容器编排** - 考虑使用 Kubernetes 或 Docker Swarm
3. **镜像优化** - 减小镜像大小，使用多阶段构建
4. **健康检查** - 完善所有服务的健康检查端点
5. **监控告警** - 集成 Prometheus + Grafana

---

## 相关资源

- Docker Buildx 文档: https://docs.docker.com/buildx/
- Multi-platform images: https://docs.docker.com/build/building/multi-platform/
- Docker Registry: https://docs.docker.com/registry/
- Docker Compose: https://docs.docker.com/compose/

---

## 技术支持

遇到问题请查看：

1. 服务器日志: `docker-compose -f docker-compose.yml logs -f`
2. 构建日志: `/tmp/build-*.log`（服务器上）
3. 网络连接: `curl http://Registry:PORT/v2/`
4. 镜像列表: `docker images | grep addp`

如需帮助，请提供：
- 操作系统和版本
- Docker 版本
- 错误日志
- 网络环境（是否有代理/防火墙）
