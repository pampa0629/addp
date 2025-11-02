# 服务器 init-db.sql 错误修复指南

## 🔍 问题分析

### 错误信息
```
Error response from daemon: mounts denied:
The path /opt/addp/scripts/init-db.sql is not shared from the host and is not known to Docker.
```

### 根本原因
服务器上的 `docker-compose.prod.yml` 文件**不是最新版本**，仍然包含：
```yaml
postgres:
  image: ${REGISTRY}/addp-infra-postgres:15-alpine  # ❌ 旧镜像
  volumes:
    - ./scripts/init-db.sql:/docker-entrypoint-initdb.d/init-db.sql  # ❌ 文件挂载
```

应该使用：
```yaml
postgres:
  image: ${REGISTRY}/addp-infra-postgres-init:15-alpine  # ✅ 新镜像（含初始化脚本）
  volumes:
    - postgres_data:/var/lib/postgresql/data  # ✅ 只挂载数据卷
    # 不再需要挂载 init-db.sql
```

---

## ✅ 解决方案

### 方案 A: 在服务器上运行修复脚本（推荐）

#### 步骤 1: 传输修复脚本到服务器

```bash
# 在开发机执行
scp scripts/fix-docker-compose.sh pampa@192.168.31.174:~/addp/scripts/
```

#### 步骤 2: 在服务器上运行修复脚本

```bash
# SSH 到服务器
ssh pampa@192.168.31.174

# 进入目录
cd ~/addp

# 运行修复脚本
chmod +x scripts/fix-docker-compose.sh
./scripts/fix-docker-compose.sh

# 脚本会：
# 1. 检查配置问题
# 2. 备份原文件
# 3. 自动修复配置
```

#### 步骤 3: 重新部署

```bash
# 方式 1: 使用部署脚本
REGISTRY=192.168.31.238:5001 ./scripts/deploy-from-registry.sh

# 方式 2: 直接启动
REGISTRY=192.168.31.238:5001 docker-compose -f docker-compose.prod.yml up -d
```

---

### 方案 B: 手动修复配置文件

如果无法传输脚本，在服务器上手动编辑：

```bash
# SSH 到服务器
ssh pampa@192.168.31.174
cd ~/addp

# 备份原文件
cp docker-compose.prod.yml docker-compose.prod.yml.backup

# 编辑文件
vim docker-compose.prod.yml
# 或
nano docker-compose.prod.yml
```

**需要修改的地方**:

1. **修改 PostgreSQL 镜像名称**（第 13 行附近）:
   ```yaml
   # 修改前:
   image: ${REGISTRY:-localhost:5000}/addp-infra-postgres:15-alpine

   # 修改后:
   image: ${REGISTRY:-localhost:5000}/addp-infra-postgres-init:15-alpine
   ```

2. **删除 init-db.sql 挂载行**（第 22 行附近）:
   ```yaml
   # 删除这一行:
   - ./scripts/init-db.sql:/docker-entrypoint-initdb.d/init-db.sql
   ```

修改后的 postgres 配置应该是：
```yaml
postgres:
  image: ${REGISTRY:-localhost:5000}/addp-infra-postgres-init:15-alpine
  container_name: addp-postgres
  environment:
    POSTGRES_DB: ${POSTGRES_DB:-addp}
    POSTGRES_USER: ${POSTGRES_USER:-addp}
    POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-addp_password}
    PGDATA: /var/lib/postgresql/data/pgdata
  volumes:
    - postgres_data:/var/lib/postgresql/data
    # ← 注意：这里不再挂载 init-db.sql
  ports:
    - "5432:5432"
  networks:
    - addp-network
  restart: unless-stopped
  healthcheck:
    test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-addp}"]
    interval: 10s
    timeout: 5s
    retries: 5
```

保存后重新部署：
```bash
REGISTRY=192.168.31.238:5001 docker-compose -f docker-compose.prod.yml up -d
```

---

### 方案 C: 重新传输最新配置文件

如果可以连接到服务器：

```bash
# 在开发机执行
scp docker-compose.prod.yml pampa@192.168.31.174:~/addp/

# 然后 SSH 到服务器重新部署
ssh pampa@192.168.31.174
cd ~/addp
REGISTRY=192.168.31.238:5001 docker-compose -f docker-compose.prod.yml up -d
```

---

## 🔍 验证修复

修复后，验证配置是否正确：

```bash
# 1. 检查镜像名称
grep "postgres:" -A3 docker-compose.prod.yml

# 应该看到:
# postgres:
#   image: ${REGISTRY:-localhost:5000}/addp-infra-postgres-init:15-alpine
#   container_name: addp-postgres

# 2. 确认没有 init-db.sql 挂载
grep "init-db.sql" docker-compose.prod.yml

# 应该没有任何输出

# 3. 验证镜像存在
docker pull 192.168.31.238:5001/addp-infra-postgres-init:15-alpine

# 应该成功拉取
```

---

## ❓ 为什么会出现这个问题

1. **旧配置文件**: 服务器上的 `docker-compose.prod.yml` 是之前传输的旧版本
2. **开发机已更新**: 开发机上的配置已更新为新的 postgres-init 镜像
3. **文件未同步**: 最新的配置文件没有传输到服务器

---

## 📋 完整部署检查清单

确保以下文件都是最新版本：

### 在开发机验证
```bash
# 检查配置正确性
grep "postgres-init" docker-compose.prod.yml  # 应该有结果
grep "init-db.sql" docker-compose.prod.yml   # 应该无结果

# 检查镜像已推送
curl http://localhost:5001/v2/addp-infra-postgres-init/tags/list
```

### 在服务器验证
```bash
# 检查文件已传输
ls -la ~/addp/docker-compose.prod.yml

# 检查配置正确性
grep "postgres-init" ~/addp/docker-compose.prod.yml  # 应该有结果
grep "init-db.sql" ~/addp/docker-compose.prod.yml   # 应该无结果

# 检查可以拉取镜像
docker pull 192.168.31.238:5001/addp-infra-postgres-init:15-alpine
```

---

## 🎯 推荐部署流程

```bash
# === 在开发机 ===

# 1. 确保所有镜像已推送
curl http://localhost:5001/v2/_catalog | jq

# 2. 传输最新配置和脚本到服务器
scp docker-compose.prod.yml pampa@192.168.31.174:~/addp/
scp scripts/deploy-from-registry.sh pampa@192.168.31.174:~/addp/scripts/
scp scripts/fix-docker-compose.sh pampa@192.168.31.174:~/addp/scripts/

# === 在服务器 ===

# 3. SSH 登录
ssh pampa@192.168.31.174

# 4. 验证配置
cd ~/addp
./scripts/fix-docker-compose.sh  # 检查并修复配置

# 5. 部署
REGISTRY=192.168.31.238:5001 ./scripts/deploy-from-registry.sh

# 6. 验证
docker-compose -f docker-compose.prod.yml ps
curl http://localhost:5170
```

---

## 📝 相关文档

- [DEPLOY_TO_SERVER.md](DEPLOY_TO_SERVER.md) - 完整部署指南
- [SCRIPT_FIXES.md](SCRIPT_FIXES.md) - 脚本修复总结
- [PUSH_INFRASTRUCTURE_IMAGES.md](PUSH_INFRASTRUCTURE_IMAGES.md) - 基础设施镜像说明

---

## 💡 技术说明

### 为什么使用嵌入式初始化脚本

**旧方案** (文件挂载):
```yaml
volumes:
  - ./scripts/init-db.sql:/docker-entrypoint-initdb.d/init-db.sql
```
❌ 依赖主机文件系统
❌ macOS Docker Desktop 需要配置文件共享
❌ 不同部署路径需要修改配置
❌ 不符合容器化最佳实践

**新方案** (嵌入镜像):
```dockerfile
# scripts/postgres/Dockerfile
FROM postgres:15-alpine
COPY init-db.sql /docker-entrypoint-initdb.d/
```
✅ 初始化脚本随镜像分发
✅ 无需主机文件
✅ 任意路径部署
✅ 真正的不可变基础设施
✅ 符合容器化最佳实践

---

现在运行修复脚本或手动修复配置文件，就可以正常部署了！🚀
