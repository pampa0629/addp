# 端口 9001 冲突解决方案

## 🔍 问题分析

**错误信息**:
```
Bind for 0.0.0.0:9001 failed: port is already allocated
```

**原因**: 服务器上端口 9001 已经被其他程序占用（很可能是之前部署的 Business MinIO Console）

---

## ✅ 快速解决方案

### 方案 A: 查看并停止占用端口的进程（推荐）

#### 步骤 1: 查看是什么占用了端口 9001

```bash
# 在服务器执行
lsof -i :9001

# 输出示例:
# COMMAND    PID  USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
# docker-pr 1234 root    4u  IPv4  12345      0t0  TCP *:9001 (LISTEN)
```

#### 步骤 2: 检查是否是 Business MinIO

```bash
# 查看 Business 基础设施
cd ~/addp/business
docker-compose -f docker-compose.prod.yml ps

# 如果看到 business-minio 在运行，说明是 Business MinIO 占用了 9001
```

#### 步骤 3: 解决冲突

**如果是 Business MinIO 占用**:

```bash
# Business MinIO 默认也使用 9001 端口
# 需要修改其中一个的端口

# 选项 1: 修改 Business MinIO 端口（推荐）
cd ~/addp/business
vim docker-compose.prod.yml

# 找到 MinIO console 端口映射，修改为:
# ports:
#   - "9002:9000"  # API
#   - "9003:9001"  # Console (改为 9003)

# 重启 Business 基础设施
docker-compose -f docker-compose.prod.yml down
docker-compose -f docker-compose.prod.yml up -d

# 选项 2: 修改 ADDP System MinIO 端口
cd ~/addp
vim docker-compose.prod.yml

# 找到 minio-system 服务，修改 console 端口:
# ports:
#   - "9000:9000"  # API
#   - "19001:9001"  # Console (改为 19001)
```

---

### 方案 B: 自动检查并修复端口冲突

#### 步骤 1: 运行端口检查脚本

```bash
# 传输脚本到服务器
# 在开发机执行:
scp scripts/check-ports.sh pampa@192.168.31.174:~/addp/scripts/

# 在服务器执行:
ssh pampa@192.168.31.174
cd ~/addp
chmod +x scripts/check-ports.sh
./scripts/check-ports.sh

# 脚本会显示所有端口占用情况和解决方案
```

#### 步骤 2: 根据脚本提示修复

脚本会告诉您哪些端口被占用，以及占用它们的进程。

---

### 方案 C: 统一规划端口（推荐用于生产环境）

创建一个清晰的端口分配方案：

#### ADDP System 端口（主系统）
```
5432  - PostgreSQL
6379  - Redis
8000  - Gateway
8080  - System Backend
8081  - Manager Backend
8082  - Meta Backend
9000  - MinIO System API
9001  - MinIO System Console
9200  - Elasticsearch
5170  - Portal
```

#### Business Infrastructure 端口（独立部署）
```
5433  - PostgreSQL (Business)
9002  - MinIO Business API
9003  - MinIO Business Console
```

#### 修改 Business 配置

```bash
# 编辑 business/docker-compose.prod.yml
cd ~/addp/business
vim docker-compose.prod.yml
```

修改 MinIO 端口部分：
```yaml
minio:
  image: ${REGISTRY:-localhost:5000}/addp-infra-minio:latest
  container_name: business-minio
  command: server /data --console-address ":9001"
  ports:
    - "9002:9000"   # API 端口
    - "9003:9001"   # Console 端口（从 9001 改为 9003）
  environment:
    MINIO_ROOT_USER: ${MINIO_ROOT_USER:-minioadmin}
    MINIO_ROOT_PASSWORD: ${MINIO_ROOT_PASSWORD:-minioadmin}
```

保存后重启：
```bash
docker-compose -f docker-compose.prod.yml down
docker-compose -f docker-compose.prod.yml up -d
```

---

## 🔧 详细修复步骤

### 修改 Business MinIO 端口（推荐）

```bash
# 1. 停止 Business 基础设施
cd ~/addp/business
docker-compose -f docker-compose.prod.yml down

# 2. 修改配置文件
sed -i.bak 's/"9001:9001"/"9003:9001"/' docker-compose.prod.yml

# 3. 验证修改
grep "9003:9001" docker-compose.prod.yml

# 4. 重启 Business 基础设施
docker-compose -f docker-compose.prod.yml up -d

# 5. 验证端口
docker ps | grep business-minio
# 应该看到: 0.0.0.0:9003->9001/tcp

# 6. 现在部署 ADDP System
cd ~/addp
REGISTRY=192.168.31.238:5001 docker-compose -f docker-compose.prod.yml up -d
```

---

## 📋 完整部署顺序

正确的部署顺序应该是：

```bash
# 1. 部署 Business 基础设施（使用不冲突的端口）
cd ~/addp/business
# 确保端口配置:
#   PostgreSQL: 5433
#   MinIO API: 9002
#   MinIO Console: 9003
docker-compose -f docker-compose.prod.yml up -d

# 2. 验证 Business 基础设施
docker-compose -f docker-compose.prod.yml ps
# 所有服务应该是 Up 状态

# 3. 部署 ADDP System
cd ~/addp
REGISTRY=192.168.31.238:5001 docker-compose -f docker-compose.prod.yml up -d

# 4. 验证 ADDP System
docker-compose -f docker-compose.prod.yml ps
```

---

## 🎯 快速命令参考

### 查看端口占用
```bash
# 查看单个端口
lsof -i :9001

# 查看所有 ADDP 相关端口
lsof -i :5432,6379,8000,8080,8081,8082,9000,9001,9200,5170

# 查看 Docker 容器端口映射
docker ps --format "table {{.Names}}\t{{.Ports}}"
```

### 停止占用端口的容器
```bash
# 查看占用端口的容器
docker ps | grep 9001

# 停止容器
docker stop <container-name>

# 或停止整个 compose 项目
cd ~/addp/business
docker-compose -f docker-compose.prod.yml down
```

### 修改端口后重新部署
```bash
# 1. 停止服务
docker-compose -f docker-compose.prod.yml down

# 2. 修改 docker-compose.prod.yml 中的端口

# 3. 重新启动
docker-compose -f docker-compose.prod.yml up -d
```

---

## ⚠️ 注意事项

1. **Business 和 ADDP System 使用不同端口**: Business 基础设施应该使用不同的端口避免冲突

2. **默认配置可能冲突**: 如果您之前按照文档部署了 Business，其 MinIO Console 默认使用 9001

3. **建议的端口规划**:
   - **5000-5999**: 开发和前端服务
   - **6000-7999**: 缓存和消息队列
   - **8000-8999**: 后端服务
   - **9000-9199**: ADDP System 基础设施
   - **9200-9999**: Business 基础设施

---

## ✅ 推荐配置

### business/docker-compose.prod.yml
```yaml
postgres:
  ports:
    - "5433:5432"  # 与 ADDP 的 5432 区分

minio:
  ports:
    - "9002:9000"  # API
    - "9003:9001"  # Console（避免与 ADDP 的 9001 冲突）
```

### docker-compose.prod.yml (ADDP System)
```yaml
postgres:
  ports:
    - "5432:5432"

minio-system:
  ports:
    - "9000:9000"  # API
    - "9001:9001"  # Console
```

---

## 🔍 验证部署成功

```bash
# 查看所有容器
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

# 访问服务
curl http://localhost:9001  # ADDP MinIO Console
curl http://localhost:9003  # Business MinIO Console
curl http://localhost:5170  # Portal
```

---

现在按照方案 A 或 B 修复端口冲突，然后重新部署即可！🚀
