# 完整镜像推送和部署指南（包含基础设施）

## 问题回顾

之前的错误原因：
- ❌ **只推送了 ADDP 自己的镜像**（system-backend、gateway 等）
- ❌ **没有推送基础设施镜像**（PostgreSQL、Redis、MinIO、Elasticsearch）
- ❌ 服务器尝试从 Docker Hub 拉取基础设施镜像时超时

## 解决方案

推送**所有镜像**到私有 Registry，包括第三方依赖。

---

## 快速部署步骤

### 在开发机执行

#### 方式 A: 一键推送所有镜像（推荐）

```bash
# 在开发机项目根目录执行
./scripts/push-all-images.sh 5001

# 脚本会自动:
# 1. 推送基础设施镜像 (postgres, redis, minio, elasticsearch)
# 2. 推送 ADDP 应用镜像 (7个服务)
```

#### 方式 B: 分步推送

```bash
# 1. 推送基础设施镜像
./scripts/push-infrastructure-images.sh 5001

# 2. 推送 ADDP 应用镜像
./scripts/push-to-local-registry.sh 5001
```

#### 验证推送结果

```bash
# 查看所有镜像
curl http://localhost:5001/v2/_catalog | jq

# 应该看到:
# {
#   "repositories": [
#     "addp-gateway",
#     "addp-infra-elasticsearch",
#     "addp-infra-minio",
#     "addp-infra-postgres",
#     "addp-infra-redis",
#     "addp-manager-backend",
#     "addp-manager-frontend",
#     "addp-meta-backend",
#     "addp-portal",
#     "addp-system-backend",
#     "addp-system-frontend"
#   ]
# }

# 检查具体镜像
docker buildx imagetools inspect localhost:5001/addp-infra-postgres:15-alpine
```

### 传输文件到服务器

```bash
# 传输更新后的配置文件
rsync -avz \
  docker-compose.prod.yml \
  business/docker-compose.prod.yml \
  scripts/deploy-from-registry.sh \
  pampa@192.168.31.174:~/addp/

# 或使用 scp
scp docker-compose.prod.yml pampa@192.168.31.174:~/addp/
scp business/docker-compose.prod.yml pampa@192.168.31.174:~/addp/business/
scp scripts/deploy-from-registry.sh pampa@192.168.31.174:~/addp/scripts/
```

### 在服务器执行

```bash
# 1. SSH 登录服务器
ssh pampa@192.168.31.174

# 2. 进入部署目录
cd ~/addp

# 3. 确保脚本可执行
chmod +x scripts/deploy-from-registry.sh

# 4. 运行部署（替换为开发机的实际 IP）
REGISTRY=192.168.31.238:5001 ./scripts/deploy-from-registry.sh

# 5. 等待所有服务启动
docker-compose -f docker-compose.prod.yml ps

# 6. 查看日志
docker-compose -f docker-compose.prod.yml logs -f
```

---

## 镜像清单

### ADDP 应用镜像 (7个)

| 镜像名 | 说明 |
|--------|------|
| addp-system-backend | 系统后端服务 |
| addp-system-frontend | 系统前端 |
| addp-manager-backend | 管理后端服务 |
| addp-manager-frontend | 管理前端 |
| addp-meta-backend | 元数据后端服务 |
| addp-gateway | API 网关 |
| addp-portal | 统一入口 |

### 基础设施镜像 (4个)

| 原镜像 | 私有 Registry 名称 |
|--------|-------------------|
| postgres:15-alpine | addp-infra-postgres:15-alpine |
| redis:7-alpine | addp-infra-redis:7-alpine |
| minio/minio:latest | addp-infra-minio:latest |
| elasticsearch:8.11.0 | addp-infra-elasticsearch:8.11.0 |

---

## 新增/修改的文件

### 新增脚本

1. **scripts/push-infrastructure-images.sh**
   - 拉取第三方基础设施镜像
   - 打标签为 `addp-infra-*`
   - 推送到私有 Registry

2. **scripts/push-all-images.sh**
   - 一键推送所有镜像（基础设施 + 应用）
   - 自动检测架构并决定是否多架构构建

### 修改的文件

1. **docker-compose.prod.yml**
   - 所有镜像改为从 `${REGISTRY}` 拉取
   - 基础设施镜像使用 `addp-infra-` 前缀

2. **business/docker-compose.prod.yml**
   - PostgreSQL 和 MinIO 改为从 `${REGISTRY}` 拉取

---

## 故障排查

### 问题 1: 基础设施镜像推送失败

```bash
# 检查网络连接
docker pull postgres:15-alpine

# 如果失败，可能是网络问题，尝试:
# 1. 配置 Docker 镜像加速器
# 2. 使用代理
# 3. 等待网络恢复
```

### 问题 2: 服务器仍然尝试从 Docker Hub 拉取

**原因**: docker-compose.prod.yml 未更新或 REGISTRY 环境变量未设置

**解决**:
```bash
# 确认文件已更新
grep "addp-infra-postgres" docker-compose.prod.yml

# 确认环境变量
echo $REGISTRY

# 重新传输文件
scp docker-compose.prod.yml server:~/addp/
```

### 问题 3: 镜像拉取慢或超时

**原因**: 镜像较大，网络慢

**解决**:
```bash
# 在服务器上提前拉取
docker pull 192.168.31.238:5001/addp-infra-elasticsearch:8.11.0
docker pull 192.168.31.238:5001/addp-infra-postgres:15-alpine

# 或调整 docker-compose 超时
docker-compose -f docker-compose.prod.yml pull --timeout 300
```

---

## 验证部署成功

```bash
# 1. 检查所有容器运行
docker-compose -f docker-compose.prod.yml ps

# 应该看到所有服务 State 为 Up:
# NAME                     STATE
# addp-postgres            Up
# addp-redis               Up
# addp-minio-system        Up
# addp-elasticsearch       Up
# addp-system-backend      Up
# addp-gateway             Up
# ...

# 2. 检查健康状态
docker-compose -f docker-compose.prod.yml ps --format json | jq -r '.[] | "\(.Name): \(.Health)"'

# 3. 访问服务
curl http://localhost:8080/health  # System backend
curl http://localhost:8000/health  # Gateway
curl http://localhost:5170         # Portal

# 4. 检查日志（确认没有错误）
docker-compose -f docker-compose.prod.yml logs --tail=50
```

---

## 生产环境注意事项

1. **镜像大小**: Elasticsearch 镜像约 1.2GB，首次拉取需要时间
2. **网络稳定性**: 确保开发机和服务器网络稳定
3. **Registry 持久化**: 确保 Registry 容器使用 volume 持久化数据
4. **定期更新**: 定期推送最新镜像到 Registry

---

## 下次更新流程

```bash
# 1. 在开发机重新构建和推送
./scripts/push-all-images.sh 5001

# 2. 在服务器重新部署
ssh server
cd ~/addp
REGISTRY=192.168.31.238:5001 docker-compose -f docker-compose.prod.yml pull
docker-compose -f docker-compose.prod.yml up -d

# 3. 验证更新
docker-compose -f docker-compose.prod.yml ps
```

---

## 相关文档

- [DEPLOY_WITH_LOCAL_REGISTRY.md](DEPLOY_WITH_LOCAL_REGISTRY.md) - 本地 Registry 部署完整指南
- [DEPLOY_SERVER_DIRECT.md](DEPLOY_SERVER_DIRECT.md) - 服务器直接构建部署（备选方案）
- [DEPLOY_SUMMARY.md](DEPLOY_SUMMARY.md) - 所有部署问题总结
