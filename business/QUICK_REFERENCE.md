# Business Infrastructure - Quick Reference

## 🚀 快速启动

```bash
cd business
./scripts/start.sh
```

## 📊 服务访问

| 服务 | URL | 默认凭据 |
|------|-----|---------|
| PostgreSQL | `localhost:5433` | business / business_password |
| MinIO Console | http://localhost:9003 | minioadmin / minioadmin |
| MinIO API | http://localhost:9002 | - |

## 🔧 常用命令

### 查看状态
```bash
cd business
docker compose ps
```

### 查看日志
```bash
# 所有服务
docker compose logs -f

# 单独服务
docker compose logs -f postgres
docker compose logs -f minio
```

### 重启服务
```bash
docker compose restart
```

### 停止服务
```bash
./scripts/stop.sh
# 或
docker compose down
```

### 完全清理（⚠️ 删除所有数据）
```bash
docker compose down -v
```

## 🔌 连接字符串

### PostgreSQL
```bash
# From host
postgresql://business:business_password@localhost:5433/business

# From Docker (ADDP services)
postgresql://business:business_password@host.docker.internal:5433/business
```

### MinIO
```bash
# From host
Endpoint: localhost:9002
Access Key: minioadmin
Secret Key: minioadmin

# From Docker (ADDP services)
Endpoint: host.docker.internal:9002
Access Key: minioadmin
Secret Key: minioadmin
```

## 🗄️ 数据库操作

### 连接到数据库
```bash
docker exec -it business-postgres psql -U business -d business
```

### 查看 schemas
```bash
docker exec business-postgres psql -U business -d business -c "\dn+"
```

### 查看表
```bash
docker exec business-postgres psql -U business -d business -c "\dt business_data.*"
```

### 备份数据库
```bash
docker exec business-postgres pg_dump -U business -d business > backup_$(date +%Y%m%d_%H%M%S).sql
```

### 恢复数据库
```bash
docker exec -i business-postgres psql -U business -d business < backup.sql
```

## 📦 MinIO 操作

### 使用 mc (MinIO Client)

#### 安装 mc
```bash
# macOS
brew install minio/stable/mc

# Linux
wget https://dl.min.io/client/mc/release/linux-amd64/mc
chmod +x mc
```

#### 配置 alias
```bash
mc alias set business-minio http://localhost:9002 minioadmin minioadmin
```

#### 常用操作
```bash
# 列出 buckets
mc ls business-minio

# 列出文件
mc ls business-minio/addp-data

# 上传文件
mc cp file.txt business-minio/addp-data/

# 下载文件
mc cp business-minio/addp-data/file.txt ./

# 备份整个 bucket
mc mirror business-minio/addp-data ./backup/
```

## 📈 监控

### 检查容器健康状态
```bash
docker ps --filter "name=business" --format "table {{.Names}}\t{{.Status}}"
```

### 查看资源使用
```bash
docker stats business-postgres business-minio
```

### 检查磁盘使用
```bash
docker system df -v | grep business
```

## 🔍 故障排查

### 服务无法启动
```bash
# 查看详细日志
docker compose logs postgres
docker compose logs minio

# 检查端口占用
lsof -i :5433
lsof -i :9002
lsof -i :9003
```

### 无法连接数据库
```bash
# 检查容器状态
docker compose ps

# 测试连接
docker exec business-postgres pg_isready -U business

# 查看错误日志
docker compose logs postgres | tail -50
```

### MinIO 无法访问
```bash
# 测试健康检查
curl http://localhost:9002/minio/health/live

# 查看日志
docker compose logs minio | tail -50
```

### 数据丢失
```bash
# 检查 volume 是否存在
docker volume ls | grep business

# 查看 volume 详情
docker volume inspect business_postgres_data
docker volume inspect business_minio_data
```

## 🔐 安全检查清单

生产环境部署前：

- [ ] 修改 PostgreSQL 密码（BUSINESS_POSTGRES_PASSWORD）
- [ ] 修改 MinIO 凭据（BUSINESS_MINIO_ROOT_USER/PASSWORD）
- [ ] 配置防火墙规则（仅允许必要端口）
- [ ] 启用 SSL/TLS（PostgreSQL 和 MinIO）
- [ ] 配置定期自动备份
- [ ] 设置磁盘使用率告警
- [ ] 限制 MinIO Console 访问 IP
- [ ] 审查数据库权限

## 📚 相关文档

- [业务基础设施架构](ARCHITECTURE.md)
- [使用文档](README.md)
- [验证报告](VERIFICATION.md)
- [主项目文档](../CLAUDE.md)
