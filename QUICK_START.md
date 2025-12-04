# ADDP 快速启动指南

## 一键启动生产环境

### 前提条件

1. Docker 和 Docker Compose 已安装
2. 本地 Registry 运行在 `localhost:5001`

```bash
# 启动 registry（如果还没有）
docker run -d -p 5001:5000 --restart=always --name registry registry:2
```

---

## 🚀 启动 ADDP

### 方法 1: 使用启动脚本（推荐）

```bash
./scripts/prod/start.sh
```

**脚本会自动**：
- ✅ 检查并创建 `.env.prod`（如果不存在）
- ✅ 自动生成安全密钥（JWT_SECRET, ENCRYPTION_KEY 等）
- ✅ 检查 registry 是否可访问
- ✅ 检查并清理端口冲突
- ✅ 启动所有服务
- ✅ 执行健康检查
- ✅ 显示访问地址和登录信息

**输出示例**:
```
========================================
ADDP Production Startup
========================================

✓ Created .env.prod with secure keys
✓ Registry is accessible
✓ Ports cleared
✓ System Backend is healthy
✓ System Frontend is healthy

========================================
ADDP Started Successfully!
========================================

Access URLs:
  System Frontend: http://localhost:8090
  System Backend:  http://localhost:8080

Super Admin Login:
  Username: SuperAdmin
  Password: 20251001#SuperAdmin
```

---

### 方法 2: 手动启动

```bash
# 1. 确保 .env.prod 存在
cp .env.prod.example .env.prod

# 2. 编辑 .env.prod，设置密钥

# 3. 启动服务
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d
```

---

## 🛑 停止 ADDP

### 使用停止脚本

```bash
./scripts/prod/stop.sh
```

### 手动停止

```bash
docker compose -f docker-compose.prod.yml down
```

---

## 📊 查看状态

```bash
# 查看服务状态
docker compose -f docker-compose.prod.yml ps

# 查看日志
docker compose -f docker-compose.prod.yml logs -f

# 查看特定服务日志
docker compose -f docker-compose.prod.yml logs -f system-backend
```

---

## 🔍 故障排查

### 问题 1: Registry 无法访问

**错误信息**:
```
Error: Docker registry not accessible at localhost:5001
```

**解决**:
```bash
# 检查 registry 是否运行
docker ps | grep registry

# 如果没有，启动 registry
docker run -d -p 5001:5000 --restart=always --name registry registry:2

# 测试连接
curl http://localhost:5001/v2/
```

---

### 问题 2: 端口冲突

**错误信息**:
```
Bind for 0.0.0.0:8080 failed: address already in use
```

**解决**:
启动脚本会自动检测并提示您清理端口，或者手动清理：

```bash
# 查看占用端口的进程
lsof -i :8080

# 杀掉进程
lsof -ti:8080 | xargs kill -9
```

---

### 问题 3: 环境变量未设置

**错误信息**:
```
WARN[0000] The "ENCRYPTION_KEY" variable is not set.
```

**解决**:
使用启动脚本会自动创建 `.env.prod`，或者：

```bash
cp .env.prod.example .env.prod
# 编辑 .env.prod，设置所有密钥
```

---

### 问题 4: 服务重启循环

**检查日志**:
```bash
docker compose -f docker-compose.prod.yml logs system-backend
```

**常见原因**:
1. ENCRYPTION_KEY 不是 base64 格式
2. 数据库连接失败
3. 端口冲突

**解决**: 使用启动脚本会自动生成正确格式的密钥

---

## 🔑 默认凭证

### 超级管理员
- **Username**: `SuperAdmin`
- **Password**: `20251001#SuperAdmin`
- **⚠️  首次登录后务必修改密码**

### MinIO
- **Access Key**: `minioadmin`
- **Secret Key**: 在 `.env.prod` 中查看

### PostgreSQL
- **User**: `addp`
- **Password**: 在 `.env.prod` 中查看
- **Database**: `addp`

---

## 📝 常用命令

```bash
# 启动
./scripts/prod/start.sh

# 停止
./scripts/prod/stop.sh

# 重启
docker compose -f docker-compose.prod.yml restart

# 查看日志
docker compose -f docker-compose.prod.yml logs -f

# 进入容器
docker compose -f docker-compose.prod.yml exec system-backend sh

# 查看数据库
docker compose -f docker-compose.prod.yml exec postgres psql -U addp -d addp
```

---

## 🎯 访问地址

| 服务 | 地址 |
|------|------|
| System Frontend | http://localhost:8090 |
| System Backend API | http://localhost:8080 |
| MinIO Console | http://localhost:9001 |
| PostgreSQL | localhost:5432 |
| Redis | localhost:6379 |

---

## ✅ 验证部署

### 1. 健康检查

```bash
curl http://localhost:8080/health
# 预期输出: {"status":"ok"}
```

### 2. 登录测试

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"SuperAdmin","password":"20251001#SuperAdmin"}'
# 预期输出: {"access_token":"...","token_type":"Bearer"}
```

### 3. 前端访问

在浏览器打开: http://localhost:8090

---

## 📚 更多文档

- [完整部署指南](docs/DEPLOYMENT.md)
- [架构说明](CLAUDE.md)
- [部署脚本说明](scripts/deploy/README.md)

---

**版本**: v0.0.6
**最后更新**: 2025-10-31
