# Business Database

## 概述

业务库为 ADDP 平台提供业务数据存储，与 ADDP 系统库完全独立部署。

**包含服务**：
- **PostgreSQL (PostGIS)**：业务数据库，端口 5433
- **MinIO**：业务对象存储，端口 9002-9003

**关键特性**：
- ✅ 独立部署，无依赖
- ✅ CPU 架构自适应（ARM64/AMD64）
- ✅ PostGIS 空间数据支持
- ✅ 幂等启动脚本

## 快速开始

```bash
# 1. 配置环境变量
cp .env.example .env
# 编辑 .env 修改密码

# 2. 启动服务
./start.sh

# 3. 验证服务
docker-compose ps
```

## 架构说明

### 与 ADDP 系统库的区别

| 组件 | ADDP 系统库 | Business 业务库 |
|------|------------|----------------|
| PostgreSQL | 端口 5432 | 端口 5433 |
| MinIO | 端口 9000-9001 | 端口 9002-9003 |
| 用途 | ADDP 元数据（用户、资源配置、任务定义） | 用户业务数据（上传的数据、文件） |
| 示例数据 | 用户账号、资源配置表 | Shapefile 空间数据表、用户上传文件 |

### CPU 架构自适应

启动脚本自动检测架构并选择最优镜像：

| CPU 架构 | PostgreSQL 镜像 | 性能 |
|---------|----------------|------|
| **ARM64** (Apple Silicon) | `imresamu/postgis-arm64:15-3.4` | ⚡ 原生性能 |
| **AMD64** (Intel/AMD) | `postgis/postgis:15-3.4` | ⚡ 原生性能 |

## 脚本说明

### start.sh - 启动服务

```bash
./start.sh
```

**功能**：检查配置、检测架构、启动服务、验证健康状态、安装 PostGIS
**特性**：幂等执行（可重复运行）

### stop.sh - 停止服务

```bash
./stop.sh
```

停止所有业务库服务。

### restart.sh - 重启服务

```bash
./restart.sh
```

检测架构、清理旧镜像、重启服务（幂等）。

## 常用操作

### 查看日志

```bash
docker-compose logs -f postgres  # PostgreSQL 日志
docker-compose logs -f minio     # MinIO 日志
```

### 数据备份

```bash
# 备份
docker exec business-postgres pg_dump -U business business > backup.sql

# 恢复
docker exec -i business-postgres psql -U business business < backup.sql
```

### PostGIS 验证

```bash
docker exec business-postgres psql -U business -d business -c "SELECT PostGIS_Version();"
```

## 生产部署

### 1. 部署到服务器

```bash
# 复制文件
scp -r business/ user@server:/opt/addp-business/

# 登录并启动
ssh user@server
cd /opt/addp-business
cp .env.example .env
vim .env  # ⚠️ 修改密码（必须！）
./start.sh
```

### 2. 安全加固（必须！）

1. **修改默认密码**：使用 `openssl rand -base64 32` 生成强密码
2. **限制网络访问**：仅允许 ADDP 系统访问
3. **定期备份**：配置 crontab 自动备份
4. **监控磁盘**：定期检查数据卷大小
5. **更新镜像**：定期运行 `docker-compose pull && ./restart.sh`

## 故障排查

### 端口冲突

```bash
lsof -nP -i :5433           # 查看占用
# 修改 .env 中的 POSTGRES_PORT
```

### 架构不匹配

```bash
./restart.sh  # 自动检测并使用正确架构
```

### 数据恢复

```bash
./stop.sh
docker volume rm business_postgres_data
./start.sh
docker exec -i business-postgres psql -U business business < backup.sql
```

## 技术细节

- **PostgreSQL**: 15.x + PostGIS 3.4.x
- **MinIO**: latest
- **网络**: business-network (bridge)
- **持久化**: Docker volumes
- **架构**: ARM64, AMD64

## 常见问题

**Q: 业务库和系统库有什么区别？**  
A: 系统库存储 ADDP 平台元数据，业务库存储用户实际业务数据。

**Q: 为什么需要 PostGIS？**  
A: 支持 Shapefile、GeoJSON 等空间数据的存储和查询。

**Q: 可以替换为云服务吗？**  
A: 可以！支持 AWS RDS/S3、阿里云 RDS/OSS 等云服务。

**Q: 脚本可以重复执行吗？**  
A: 可以！所有脚本都是幂等的。
