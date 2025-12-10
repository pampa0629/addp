# Utils 脚本说明

本目录包含 ADDP 项目的通用工具脚本，用于批量操作、验证检查和开发辅助。

## 📋 脚本清单

### ports-validate.sh
**用途**: 验证端口分配是否符合 ADDP 规范

**功能**:
- 检查系统 MinIO 端口 (9000-9001)
- 检查业务 MinIO 端口 (9002-9003)
- 检查 PostgreSQL 端口 (5432-5433)
- 验证端口配置与实际运行容器一致性
- 彩色输出状态报告

**端口规范**:
- **系统 MinIO**: API 端口 9000，Console 端口 9001
- **业务 MinIO**: API 端口 9002，Console 端口 9003
- **系统 PostgreSQL**: 5432
- **业务 PostgreSQL**: 5433

**命令**:
```bash
./scripts/utils/ports-validate.sh
```

**预期输出**:
```
Business (.env or defaults):
  ✓ BUSINESS_MINIO_API_PORT: 9002
  ✓ BUSINESS_MINIO_CONSOLE_PORT: 9003
  ✓ BUSINESS_POSTGRES_PORT: 5433

Runtime containers (if any):
  minio    0.0.0.0:9000->9000/tcp
  business-minio    0.0.0.0:9002->9000/tcp

Policy OK. No changes required.
```

**使用场景**: 部署前配置验证、端口冲突排查

---

### go-mod-tidy-all.sh
**用途**: 批量清理所有 Go 模块的依赖

**功能**:
- 遍历所有后端模块目录
- 执行 `go mod tidy` 清理依赖
- 统一依赖版本，移除未使用的包
- 避免依赖冲突

**命令**:
```bash
./scripts/utils/go-mod-tidy-all.sh
```

**使用场景**:
- 切换分支后依赖冲突
- 更新依赖版本
- CI/CD 构建前清理

---

### test-tile-api.sh
**用途**: 测试 Manager 模块的 MVT 瓦片生成和缓存

**功能**:
- 请求指定图层的 MVT 瓦片
- 验证瓦片格式正确性
- 测试缓存命中率
- 支持自定义表名、缩放级别

**命令**:
```bash
# 交互式运行（会提示输入参数）
./scripts/utils/test-tile-api.sh

# 指定参数（计划支持）
# export TOKEN="your-jwt-token"
# export TABLE="dltb"
# export ZOOM=10
# ./scripts/utils/test-tile-api.sh
```

**预期输出**:
```
[✓] 瓦片生成成功 (200 OK)
[✓] Content-Type: application/x-protobuf
[✓] 缓存到 MinIO 成功
[✓] 第二次请求命中缓存 (<10ms)
```

**注意**: 此脚本当前为交互式，需要手动输入 JWT token 和参数。

**使用场景**: MVT 瓦片功能测试、缓存性能验证

---

### standardize-frontend-docker.sh
**用途**: 统一所有前端模块的 Dockerfile 和 nginx.conf

**功能**:
- 检查所有前端 Dockerfile 的一致性
- 验证 nginx.conf 配置规范
- 自动修复不符合标准的配置（使用 --fix 参数）
- 确保所有前端使用相同的构建模式

**检查项**:
- ✅ Dockerfile 存在
- ✅ .dockerignore 存在
- ✅ nginx.conf 存在
- ✅ 配置文件格式正确

**命令**:
```bash
# CHECK 模式（只检查，不修改）
./scripts/utils/standardize-frontend-docker.sh

# FIX 模式（自动创建缺失的文件）
./scripts/utils/standardize-frontend-docker.sh --fix

# 通过 Makefile 使用
make check-frontend      # 检查模式
make fix-frontend        # 修复模式
```

**预期输出**:
```
========================================
Frontend Docker Standardization Check
========================================

Checking: portal/frontend
  ✓ Dockerfile exists
  ✓ .dockerignore exists
  ✓ nginx.conf exists

Checking: system/frontend
  ✓ Dockerfile exists
  ✗ .dockerignore missing  [run with --fix to create]
  ✓ nginx.conf exists

Summary: 2/7 frontends compliant
```

**使用场景**:
- 前端 Docker 配置审查
- CI/CD 构建前验证
- 新增前端模块时确保规范

---

## 🔄 典型使用场景

### 场景 1: 部署前验证

```bash
# 1. 验证端口配置
./scripts/utils/ports-validate.sh

# 2. 清理 Go 依赖
./scripts/utils/go-mod-tidy-all.sh

# 3. 检查前端配置
make check-frontend
```

### 场景 2: 开发调试

```bash
# 测试 MVT 瓦片功能
./scripts/utils/test-tile-api.sh

# 查看端口占用情况
./scripts/utils/ports-validate.sh
```

### 场景 3: CI/CD 集成

```bash
# 构建前准备
./scripts/utils/go-mod-tidy-all.sh
./scripts/utils/standardize-frontend-docker.sh --fix

# 验证配置
./scripts/utils/ports-validate.sh
```

---

## ⚠️ 注意事项

1. **ports-validate.sh**
   - 需要读取 `business/.env` 文件获取业务端口配置
   - 会检查实际运行的 Docker 容器端口映射
   - 端口冲突会导致脚本退出（exit 1）

2. **go-mod-tidy-all.sh**
   - 需要在项目根目录或有权限访问所有模块的位置运行
   - 执行时间较长（取决于网络速度）
   - 会修改 `go.mod` 和 `go.sum` 文件

3. **test-tile-api.sh**
   - 需要 Manager backend 和基础设施正在运行
   - 需要有效的 JWT token
   - 测试表名需要在数据库中存在

4. **standardize-frontend-docker.sh**
   - `--fix` 模式会创建文件，请谨慎使用
   - 检查所有 frontend 目录: portal, system, manager, meta, transfer, orchestrator, develop
   - 不会覆盖已存在的配置文件

---

## 🔗 相关文档

- [基础设施管理](../infra/README.md) - 启动 PostgreSQL、Redis、MinIO
- [开发环境](../dev/README.md) - 本地开发启动流程
- [构建脚本](../build/README.md) - 编译和镜像构建

---

## 📞 故障排查

### 问题 1: ports-validate.sh 报错端口冲突

**症状**: `Policy mismatch detected`

**原因**: 实际端口配置与规范不符

**解决**:
```bash
# 检查 business/.env 配置
cat business/.env | grep PORT

# 检查运行中的容器
docker ps --format 'table {{.Names}}\t{{.Ports}}'

# 修正配置后重启
docker-compose down && docker-compose up -d
```

### 问题 2: go-mod-tidy-all.sh 执行缓慢

**原因**: 需要下载依赖，网络速度慢

**解决**:
```bash
# 配置 Go 代理加速
export GOPROXY=https://goproxy.cn,direct
./scripts/utils/go-mod-tidy-all.sh
```

### 问题 3: test-tile-api.sh 无法运行

**原因**: Backend 服务未启动或 token 过期

**解决**:
```bash
# 1. 启动服务
./scripts/dev/start.sh

# 2. 获取新的 token
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "123456"}'

# 3. 使用新 token 运行测试
./scripts/utils/test-tile-api.sh
```
