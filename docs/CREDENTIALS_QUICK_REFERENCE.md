# Infrastructure Credentials - Quick Reference Card

快速参考卡 - 基础设施访问凭据（开发环境）

---

## 🔐 ADDP 系统基础设施

### PostgreSQL
```
Host: localhost:5432
User: addp
Pass: addp_password
DB:   addp
```
```bash
psql -h localhost -p 5432 -U addp -d addp
```

### Redis
```
Host: localhost:6379
Pass: addp_redis
```
```bash
redis-cli -h localhost -p 6379 -a addp_redis
```

### MinIO
```
Console: http://localhost:9003
API:     http://localhost:9002
User:    minioadmin
Pass:    minioadmin
```

### Elasticsearch
```
HTTP: http://localhost:9200
(No auth in dev)
```

---

## 🗄️ Business 业务基础设施

### PostgreSQL
```
Host: localhost:5433  ⚠️ Port 5433!
User: business
Pass: business_password
DB:   business
```
```bash
psql -h localhost -p 5433 -U business -d business
```

### MinIO
```
Console: http://localhost:9001  ⚠️ Port 9001!
API:     http://localhost:9000  ⚠️ Port 9000!
User:    minioadmin
Pass:    minioadmin
```

---

## 📊 端口对照表

| 服务 | ADDP | Business |
|------|------|----------|
| PostgreSQL | **5432** | **5433** |
| MinIO API | **9002** | **9000** |
| MinIO Console | **9003** | **9001** |

---

## 🚀 Docker 快速命令

```bash
# 查看所有容器
docker ps

# ADDP 系统数据库
docker exec -it addp-postgres psql -U addp -d addp

# Business 业务数据库
docker exec -it business-postgres psql -U business -d business

# Redis
docker exec -it addp-redis redis-cli -a addp_redis

# MinIO 控制台
open http://localhost:9003  # ADDP
open http://localhost:9001  # Business
```

---

## ⚠️ 生产环境提醒

**必须修改以下默认密码**:
- [ ] POSTGRES_PASSWORD
- [ ] BUSINESS_POSTGRES_PASSWORD
- [ ] REDIS_PASSWORD
- [ ] MINIO_ROOT_USER/PASSWORD
- [ ] BUSINESS_MINIO_ROOT_USER/PASSWORD

**生成安全密码**:
```bash
openssl rand -base64 32
```

---

详细文档: [INFRASTRUCTURE_CREDENTIALS.md](INFRASTRUCTURE_CREDENTIALS.md)
