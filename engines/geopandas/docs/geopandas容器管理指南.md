# GeoPandas Engine 容器管理指南

本文档说明如何在 ADDP 平台的 local 和 production 部署模式下管理 geopandas-engine 容器。

## 📋 已集成的脚本

### 1. 构建脚本 (scripts/build/)

#### ✅ scripts/build/build-images.sh

**支持状态**: ✅ 完全支持

**使用方式**:
```bash
# 构建 geopandas-engine 镜像
bash scripts/build/build-images.sh --services geopandas-engine

# 构建所有服务 (包括 geopandas-engine)
bash scripts/build/build-images.sh
```

**集成内容**:
1. **智能缓存检查** (397-407 行):
   - 监控 `*.py`、`requirements.txt`、`Dockerfile` 的修改时间
   - 文件有变化时自动触发重新构建
   - 排除 `venv/` 和 `__pycache__/` 目录

2. **构建逻辑** (504-513 行):
   - 使用 `engines/geopandas/Dockerfile` 作为构建配置
   - 支持 ARM64 和 AMD64 架构
   - 自动推送到本地 registry (localhost:5001)

3. **服务列表** (593 行):
   - 已添加到标准服务列表: `"geopandas-engine:engines/geopandas"`

### 2. Local 部署脚本 (scripts/local/)

#### ✅ scripts/local/start.sh

**支持状态**: ✅ 完全支持

**修改内容**:
1. **镜像检查** (71 行):
   ```bash
   "${REGISTRY}/addp-geopandas-engine:${IMAGE_TAG}"
   ```
   - 启动前验证 geopandas-engine 镜像是否存在
   - 缺失时提示用户先构建镜像

2. **健康检查** (193-196 行):
   ```bash
   if docker compose -f docker-compose.yml ps geopandas-engine | grep -q "Up"; then
       wait_for_health "http://localhost:8099/health" "GeoPandas Engine" 60
   fi
   ```
   - 等待服务启动并通过健康检查 (最长 60 秒)

3. **访问地址显示** (221-223 行):
   ```bash
   echo -e "${GREEN}Engines:${NC}"
   echo -e "  ${CYAN}GeoPandas Engine:${NC}     http://localhost:8099"
   ```
   - 启动成功后显示访问地址

**使用方式**:
```bash
# 启动所有服务 (包括 geopandas-engine)
bash scripts/local/start.sh

# 服务会自动通过 docker-compose.yml 启动 geopandas-engine
```

#### ✅ scripts/local/stop.sh

**支持状态**: ✅ 完全支持

**说明**:
- 无需修改，使用 `docker compose down` 会自动停止所有服务包括 geopandas-engine

**使用方式**:
```bash
# 停止应用层 (包括 geopandas-engine)
bash scripts/local/stop.sh

# 停止所有服务 (应用层 + 基础设施)
bash scripts/local/stop.sh --all

# 停止并删除数据卷
bash scripts/local/stop.sh --all --volumes
```

#### ✅ scripts/local/restart.sh

**支持状态**: ✅ 完全支持

**说明**:
- 无需修改，通过调用 stop.sh 和 start.sh 实现重启
- geopandas-engine 会随其他服务一起重启

**使用方式**:
```bash
# 重启应用层 (包括 geopandas-engine)
bash scripts/local/restart.sh

# 重启所有服务 (应用层 + 基础设施)
bash scripts/local/restart.sh --all
```

### 3. Production 部署脚本 (scripts/prod/)

#### ✅ scripts/prod/start.sh

**支持状态**: ✅ 完全支持

**修改内容**:
1. **启动流程** (61 行):
   ```bash
   docker compose -f docker-compose.yml up -d \
     manager-backend \
     ...
     geopandas-engine \
     gateway
   ```
   - 在第三步与其他业务后端一起启动

2. **健康检查列表** (76 行):
   ```bash
   services=(
     ...
     "geopandas-engine:8099"
     "gateway:8000"
   )
   ```
   - 启动后自动检查 geopandas-engine 健康状态 (最长 30 秒)

3. **访问地址显示** (163 行):
   ```bash
   echo -e "  GeoPandas Engine:       http://localhost:8099"
   ```
   - 启动成功后显示访问地址

**使用方式**:
```bash
# 启动生产环境所有服务
bash scripts/prod/start.sh

# 服务启动顺序:
# 1. 基础设施层 (postgres, redis, minio, meilisearch)
# 2. System Backend (其他服务依赖)
# 3. 业务后端 + geopandas-engine + gateway
# 4. 前端服务 + nginx
```

#### ✅ scripts/prod/stop.sh

**支持状态**: ✅ 完全支持

**说明**:
- 无需修改，使用 `docker compose down` 会自动停止所有服务包括 geopandas-engine

**使用方式**:
```bash
# 停止生产环境服务
bash scripts/prod/stop.sh
```

#### ✅ scripts/prod/health-check.sh

**支持状态**: ✅ 完全支持

**修改内容** (53-58 行):
```bash
# GeoPandas Engine
if curl -f http://localhost:8099/health > /dev/null 2>&1; then
  echo -e "${GREEN}✓ geopandas-engine${NC}"
else
  echo -e "${RED}✗ geopandas-engine${NC}"
fi
```

**使用方式**:
```bash
# 检查所有服务健康状态 (包括 geopandas-engine)
bash scripts/prod/health-check.sh

# 输出示例:
# === 基础设施层健康检查 ===
# ✓ PostgreSQL
# ✓ Redis
# ✓ MinIO
# ✓ Meilisearch
#
# === 应用服务层健康检查 ===
# ✓ system-backend
# ...
# ✓ geopandas-engine
# ✓ gateway
```

## 🚀 快速使用指南

### Local 开发环境

```bash
# 1. 构建 geopandas-engine 镜像 (首次或代码修改后)
bash scripts/build/build-images.sh --services geopandas-engine

# 2. 启动开发环境 (包括 geopandas-engine)
bash scripts/local/start.sh

# 3. 验证服务状态
curl http://localhost:8099/health
# 或
bash scripts/local/status.sh

# 4. 查看日志
docker compose logs -f geopandas-engine

# 5. 停止服务
bash scripts/local/stop.sh
```

### Production 环境

```bash
# 1. 确保镜像已构建并推送到 registry
bash scripts/build/build-images.sh

# 2. 启动生产环境
bash scripts/prod/start.sh

# 3. 健康检查
bash scripts/prod/health-check.sh

# 4. 查看日志
docker compose -f docker-compose.yml logs -f geopandas-engine

# 5. 停止服务
bash scripts/prod/stop.sh
```

## 📦 Docker Compose 直接管理

如果需要单独管理 geopandas-engine 容器，也可以直接使用 docker-compose:

```bash
# 启动
docker-compose up -d geopandas-engine

# 重启
docker-compose restart geopandas-engine

# 停止
docker-compose stop geopandas-engine

# 查看日志
docker-compose logs -f geopandas-engine

# 查看状态
docker-compose ps geopandas-engine

# 进入容器
docker-compose exec geopandas-engine bash

# 重新构建并启动
docker-compose up -d --build geopandas-engine
```

## 🔍 健康检查

### 端点信息

- **URL**: `http://localhost:8099/health`
- **超时**: 60 秒 (local), 30 秒 (prod)
- **重试**: 每 2 秒检查一次

### 响应示例

```json
{
  "service": "geopandas-engine",
  "status": "healthy",
  "version": "1.0.0"
}
```

### 手动健康检查

```bash
# 简单检查
curl http://localhost:8099/health

# 带失败处理
curl -f http://localhost:8099/health || echo "Service unhealthy"

# 查看可用算子
curl http://localhost:8099/api/operators | jq .
```

## ⚠️ 注意事项

1. **镜像构建**:
   - 首次使用前必须先构建镜像: `bash scripts/build/build-images.sh --services geopandas-engine`
   - 修改代码后需要重新构建

2. **依赖服务**:
   - geopandas-engine 依赖 System Backend，确保 System Backend 先启动
   - docker-compose.yml 中已配置 `depends_on` 确保启动顺序

3. **端口占用**:
   - 确保端口 8099 未被其他服务占用
   - 修改端口需要同步更新 docker-compose.yml 和环境变量

4. **健康检查**:
   - 服务启动可能需要 30-60 秒（下载依赖、初始化）
   - 健康检查失败时查看日志: `docker-compose logs geopandas-engine`

5. **架构兼容**:
   - 当前镜像根据构建平台自动选择架构 (ARM64/AMD64)
   - 跨平台部署需要重新构建或使用多架构镜像

## 📝 修改总结

### scripts/build/build-images.sh
- ✅ 添加 geopandas-engine 智能缓存检查
- ✅ 添加 geopandas-engine 构建逻辑
- ✅ 添加到服务列表

### scripts/local/start.sh
- ✅ 添加镜像存在性检查
- ✅ 添加健康检查等待
- ✅ 添加访问地址显示

### scripts/local/stop.sh
- ✅ 无需修改 (通用逻辑已支持)

### scripts/local/restart.sh
- ✅ 无需修改 (通用逻辑已支持)

### scripts/prod/start.sh
- ✅ 添加到启动流程
- ✅ 添加到健康检查列表
- ✅ 添加访问地址显示

### scripts/prod/stop.sh
- ✅ 无需修改 (通用逻辑已支持)

### scripts/prod/health-check.sh
- ✅ 添加健康检查逻辑

## 🎯 下一步

目前所有容器管理脚本已完全支持 geopandas-engine，可以直接使用。

如需进一步优化，可以考虑:
- [ ] 添加独立的 geopandas-engine 启动/停止脚本
- [ ] 支持多 geopandas-engine 实例的负载均衡
- [ ] 添加性能监控和指标收集
- [ ] 集成到 CI/CD 流程

---

**文档创建时间**: 2025-12-22
**状态**: ✅ 所有脚本已完成集成
**测试状态**: ✅ 容器启动和健康检查通过
