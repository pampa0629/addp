# Business Infrastructure - Architecture Support

## 📋 概述

Business 基础设施支持多种 CPU 架构，包括 AMD64 (x86_64) 和 ARM64 (Apple Silicon)。

## 🏗️ 架构策略

### PostGIS 镜像选择

**最终选择**: `postgis/postgis:15-3.4`

**理由**:
- ✅ 官方维护，质量有保证
- ✅ 原生支持 AMD64 和 ARM64 架构
- ✅ 开箱即用，包含完整的 PostGIS 扩展
- ✅ 零维护成本，自动获取安全更新
- ✅ 镜像大小合理（~400MB），性能优异

**替代方案** (未采用):
- `postgres:15-alpine` + 手动编译 PostGIS
  - ❌ 编译时间长（5-10分钟）
  - ❌ 多架构支持复杂
  - ❌ 维护成本高

## 🚀 使用方法

### 1. 验证当前架构

```bash
cd business
./scripts/verify-arch.sh
```

输出示例：
```
🖥️  System Architecture: arm64
🎯 Expected Docker Arch: arm64

📦 Checking running containers...
  PostgreSQL:
    Container: business-postgres
    Image:     postgis/postgis:15-3.4
    Arch:      arm64
    Status:    ✓ MATCH
```

### 2. 重启服务（自动适配架构）

```bash
cd business
./scripts/restart.sh
```

脚本会自动：
1. 检测当前 CPU 架构 (arm64/amd64)
2. 拉取对应架构的镜像
3. 停止旧容器
4. 启动新容器（使用正确架构）

### 3. 首次启动

```bash
cd business
./scripts/start.sh
```

启动后会自动安装 PostGIS 扩展。

## 🔧 技术细节

### Docker Compose 配置

`docker-compose.yml` 中的关键配置：

```yaml
postgres:
  image: postgis/postgis:15-3.4
  platform: ${DOCKER_DEFAULT_PLATFORM:-linux/arm64}  # 动态架构检测
```

- `DOCKER_DEFAULT_PLATFORM` 环境变量由 `restart.sh` 自动设置
- 默认值 `linux/arm64` 适用于 Apple Silicon Mac
- 在 AMD64 服务器上会自动切换为 `linux/amd64`

### 架构检测逻辑

`restart.sh` 中的架构映射：

```bash
case "$(uname -m)" in
    x86_64)   DOCKER_ARCH="linux/amd64"  ;;
    arm64)    DOCKER_ARCH="linux/arm64"  ;;
    aarch64)  DOCKER_ARCH="linux/arm64"  ;;
esac
```

### PostGIS 扩展

自动安装的扩展（`install-postgis.sh`）：

- `postgis` - 核心空间数据库扩展
- `postgis_topology` - 拓扑数据支持
- `postgis_raster` - 栅格数据支持
- `fuzzystrmatch` - 模糊字符串匹配
- `postgis_tiger_geocoder` - 地理编码

## 📊 架构对比

| 架构 | 开发环境 | 生产环境 | 镜像拉取速度 | 性能 |
|------|---------|---------|------------|------|
| ARM64 | Apple Silicon Mac | AWS Graviton | 快 | 原生，优秀 |
| AMD64 | Intel/AMD PC | 大多数云服务器 | 快 | 原生，优秀 |
| 混合 (amd64 on arm64) | ⚠️ 模拟运行 | ❌ 不推荐 | 慢 | 差（需要模拟） |

## 🛠️ 故障排查

### 问题：容器运行缓慢

**原因**: 可能是架构不匹配，Docker 在模拟运行。

**解决方案**:
```bash
# 1. 验证架构
./scripts/verify-arch.sh

# 2. 如果不匹配，重启服务
./scripts/restart.sh
```

### 问题：PostGIS 扩展未安装

**解决方案**:
```bash
# 手动安装扩展
cd business
./scripts/install-postgis.sh
```

### 问题：镜像拉取失败

**可能原因**:
1. 网络连接问题
2. Docker Hub 限流

**解决方案**:
```bash
# 使用代理或镜像加速器
export DOCKER_DEFAULT_PLATFORM=linux/arm64  # 根据实际架构
docker pull --platform=linux/arm64 postgis/postgis:15-3.4
```

## 📦 镜像信息

### PostGIS 15-3.4

- **官方仓库**: https://hub.docker.com/r/postgis/postgis
- **PostgreSQL 版本**: 15.x
- **PostGIS 版本**: 3.4.x
- **支持架构**: amd64, arm64
- **基础镜像**: Debian (非 Alpine)
- **镜像大小**: ~400MB

### MinIO Latest

- **官方仓库**: https://hub.docker.com/r/minio/minio
- **支持架构**: amd64, arm64, ppc64le, s390x
- **镜像大小**: ~150MB

## 🔄 升级路径

### 升级 PostGIS 版本

1. 修改 `docker-compose.yml`:
   ```yaml
   image: postgis/postgis:16-3.5  # 新版本
   ```

2. 备份数据:
   ```bash
   docker exec business-postgres pg_dump -U business business > backup.sql
   ```

3. 重启服务:
   ```bash
   ./scripts/restart.sh
   ```

### 迁移到不同架构

例如：从开发环境 (ARM64) 迁移到生产环境 (AMD64)

1. 导出数据:
   ```bash
   docker exec business-postgres pg_dump -U business business > data.sql
   ```

2. 在新服务器上部署:
   ```bash
   cd business
   cp .env.example .env
   # 编辑 .env 配置
   ./scripts/start.sh
   ```

3. 导入数据:
   ```bash
   docker exec -i business-postgres psql -U business business < data.sql
   ```

## 📚 参考资料

- [PostGIS 官方文档](https://postgis.net/documentation/)
- [Docker 多架构支持](https://docs.docker.com/build/building/multi-platform/)
- [PostgreSQL ARM64 优化](https://www.postgresql.org/docs/current/arm64.html)

## ✅ 最佳实践

1. **开发环境**: 使用与生产环境相同的架构（如果可能）
2. **镜像拉取**: 始终显式指定 `--platform` 参数
3. **性能测试**: 在目标架构上进行性能测试
4. **定期更新**: 保持镜像版本更新，获取安全补丁
5. **架构验证**: 部署后运行 `verify-arch.sh` 确认架构正确

## 🤝 贡献

如果发现架构兼容性问题，请提交 Issue 或 PR。
