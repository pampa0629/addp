# 推送脚本修复总结

## ✅ 已修复的问题

### 1. 构建上下文错误
**问题**: Backend 服务（system-backend、manager-backend、meta-backend、gateway）的 Dockerfile 需要访问项目根目录的 `common/` 模块，但脚本使用了错误的构建上下文（子目录）。

**修复**:
- Backend 服务：`CONTEXT="."` (项目根目录)
- Frontend 服务：`CONTEXT="<service>/frontend"` (子目录)

### 2. Portal 镜像缺失
**问题**: Portal Dockerfile 和 nginx.conf 缺失

**修复**:
- 创建了 `portal/frontend/Dockerfile`
- 创建了 `portal/frontend/nginx.conf`
- 手动构建并推送了 Portal 镜像

### 3. 基础设施镜像缺失
**问题**: PostgreSQL、Redis、MinIO、Elasticsearch 未推送到私有 Registry

**修复**:
- 创建了 `scripts/push-infrastructure-images.sh`
- 推送了 4 个基础设施镜像到 Registry
- 更新了 `docker-compose.prod.yml` 使用私有 Registry 的基础设施镜像

---

## 📝 脚本验证结果

### push-to-local-registry-multiarch-cached.sh ✅

**服务列表** (7个):
- system-backend
- system-frontend
- manager-backend
- manager-frontend
- meta-backend
- gateway
- portal

**构建上下文**:
```
system-backend:    DOCKERFILE=system/backend/Dockerfile    CONTEXT=.
system-frontend:   DOCKERFILE=system/frontend/Dockerfile   CONTEXT=system/frontend
manager-backend:   DOCKERFILE=manager/backend/Dockerfile   CONTEXT=.
manager-frontend:  DOCKERFILE=manager/frontend/Dockerfile  CONTEXT=manager/frontend
meta-backend:      DOCKERFILE=meta/backend/Dockerfile      CONTEXT=.
gateway:           DOCKERFILE=gateway/Dockerfile           CONTEXT=.
portal:            DOCKERFILE=portal/frontend/Dockerfile   CONTEXT=portal/frontend
```

✅ **所有配置正确**

### push-to-local-registry-multiarch.sh ✅

**构建上下文** (已修复):
```
system-backend:    CONTEXT=.
manager-backend:   CONTEXT=.
meta-backend:      CONTEXT=.
gateway:           CONTEXT=.
```

✅ **Backend 服务使用项目根目录**

### push-to-local-registry.sh ✅

使用 `make docker-build-all` 构建，Makefile 已正确配置上下文，无需修改。

---

## 🎯 Registry 当前状态

**总镜像数**: 11 个

### ADDP 应用镜像 (7个)
1. ✅ addp-system-backend
2. ✅ addp-system-frontend
3. ✅ addp-manager-backend
4. ✅ addp-manager-frontend
5. ✅ addp-meta-backend
6. ✅ addp-gateway
7. ✅ addp-portal

### 基础设施镜像 (4个)
1. ✅ addp-infra-postgres:15-alpine
2. ✅ addp-infra-redis:7-alpine
3. ✅ addp-infra-minio:latest
4. ✅ addp-infra-elasticsearch:8.11.0

---

## 🚀 使用方法

### 推荐使用方式

```bash
# 在开发机执行

# 1. 推送所有镜像（应用 + 基础设施）
./scripts/push-all-images.sh 5001

# 或分步执行:

# 2a. 推送基础设施镜像
./scripts/push-infrastructure-images.sh 5001

# 2b. 推送应用镜像（带缓存，跳过已存在的）
./scripts/push-to-local-registry-multiarch-cached.sh 5001

# 2c. 强制重建所有镜像
./scripts/push-to-local-registry-multiarch-cached.sh 5001 --force
```

### 验证推送结果

```bash
# 查看所有镜像
curl http://localhost:5001/v2/_catalog | jq

# 检查具体镜像架构
docker buildx imagetools inspect localhost:5001/addp-portal:latest
```

---

## ✅ 所有脚本现在都已正确配置

1. ✅ **push-to-local-registry-multiarch-cached.sh** - 构建上下文已修复，包含 Portal
2. ✅ **push-to-local-registry-multiarch.sh** - 构建上下文已修复
3. ✅ **push-to-local-registry.sh** - 使用 Makefile，无需修改
4. ✅ **push-infrastructure-images.sh** - 新创建，推送基础设施镜像
5. ✅ **push-all-images.sh** - 新创建，一键推送所有镜像

现在可以使用任何脚本正确构建和推送镜像了！
