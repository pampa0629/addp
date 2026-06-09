# ADDP 开发脚本

本目录包含所有与 ADDP 开发模式相关的启动、停止、重启脚本。

## 核心原则

遵循以下7个原则(与 scripts/infra/ 一致):

1. **单一职责**: 同样功能只在一处实现
2. **适应性**: 自动适配不同环境
3. **清晰明了**: 一看就懂的结构和命名
4. **可重复执行**: 幂等性,多次执行不会破坏系统
5. **易用性**: 用户无需了解技术细节
6. **分散和集中**: 模块配置分散,脚本集中在 scripts/
7. **敢于删除**: 删除重复或无用内容

## 目录结构

```
scripts/dev/
├── README.md          # 本文件
├── start.sh           # 启动完整开发环境(后台运行)
├── stop.sh            # 停止所有开发服务
├── restart.sh         # 重启所有服务
├── keepalive.sh       # 托管命令环境前台保活入口
├── modtidy.sh         # 清理 Go 模块依赖
├── clean.sh           # 清理开发环境编译产物
└── install-frontend-deps.sh # 安装前端依赖
```

## 使用方法

### 快速启动(推荐)

```bash
# 一键启动完整环境(基础设施 + 后端 + 前端)
bash scripts/dev/start.sh

# 访问服务
# - Console: http://localhost:5170
# - System: http://localhost:5173
# - Manager: http://localhost:5174
# - API Gateway: http://localhost:8000
```

### 单独脚本使用

```bash
# 启动开发环境
bash scripts/dev/start.sh
# 自动执行:
# 0. Go 依赖检查(go mod tidy,可跳过)
# 1. 启动基础设施(PostgreSQL, Redis, MinIO, Meilisearch)
# 2-7. 启动后端服务(System, Manager, Meta, Transfer, Workers, Orchestrator, Gateway)
# 8. 启动前端服务(Console, System, Manager, Meta, Transfer, Orchestrator)

# 停止所有服务
bash scripts/dev/stop.sh
# 自动执行:
# 1. 读取 PID 文件,优雅停止进程
# 2. 清理 Vite 缓存
# 3. 清理孤立进程

# 重启服务
bash scripts/dev/restart.sh
# 等同于: stop.sh + start.sh

# 在 Codex 等托管命令环境中启动并前台保活
bash scripts/dev/keepalive.sh restart -orchestrator
# Ctrl+C 退出时会自动调用 stop.sh 清理服务

# 清理 Go 依赖
bash scripts/dev/modtidy.sh
# 对所有 Go 模块执行 go mod tidy
```

## 脚本详细说明

### start.sh

**功能**: 启动完整开发环境

**执行步骤**:
1. **Step 0**: Go 依赖检查(`go mod tidy`,可跳过)
2. **Step 1**: 启动基础设施(调用 `scripts/infra/up.sh`)
   - **智能跳过**: 如果基础设施已运行,自动跳过此步骤
   - **重启提示**: 提示用户如何手动重启基础设施
3. **Step 2-7**: 启动后端服务(依赖顺序启动)
   - System Backend (8180) - 其他服务依赖它
   - Manager Backend (8081)
   - Meta Backend (8082)
   - Transfer Backend (8083)
   - Transfer Worker + Meta Worker + Manager Worker
   - Orchestrator Backend (8084)
   - Gateway (8000) - API 路由
4. **健康检查**: 等待所有 /health 返回 200
5. **Step 8**: 启动前端服务(Console, System, Manager, Meta, Transfer, Orchestrator)
6. **输出**: 显示所有访问地址和 PID

**环境变量**:
```bash
# 跳过 Go 依赖检查(节省5-10秒启动时间)
SKIP_MODTIDY=1 bash scripts/dev/start.sh
```

**日志位置**:
- 所有日志: `logs/*.log`
- 示例: `logs/system-backend.log`, `logs/manager-backend.log`

**PID 文件**:
- 所有 PID: `.dev-pids/*.pid`
- 示例: `.dev-pids/system-backend.pid`

### stop.sh

**功能**: 停止所有开发服务

**执行步骤**:
1. 读取 PID 文件(`.dev-pids/*.pid`)
2. 优雅停止进程(`kill -TERM`)
3. 等待进程退出(最多10秒)
4. 强制杀死未退出进程(`kill -9`)
5. 清理 Vite 缓存(`node_modules/.vite/`)
6. 清理孤立的 `go run` 进程
7. 删除 PID 文件

**安全性**:
- ✅ 仅杀死由 start.sh 启动的进程(通过 PID 精确匹配)
- ✅ 不会影响其他 Go 项目或 Vite 进程

### restart.sh

**功能**: 重启所有服务(stop + start)

**实现**:
```bash
#!/usr/bin/env bash
bash scripts/dev/stop.sh
bash scripts/dev/start.sh
```

**使用场景**:
- 修改代码后需要重启
- 服务异常需要重置

**重要**: `restart.sh` 不会重启基础设施容器(PostgreSQL, Redis, MinIO, Meilisearch)
- 原因: 避免 pgvector 等扩展需要重新编译安装
- 如需重启基础设施: `bash scripts/infra/down.sh && bash scripts/infra/up.sh`
- `start.sh` 会自动检测基础设施运行状态,已运行则跳过

### keepalive.sh

**功能**: 为 Codex 等托管命令环境提供前台保活入口。

**背景**: `start.sh` 和 `restart.sh` 会以后台进程方式启动开发服务。在普通终端中脚本退出后后台服务会继续运行；但 Codex 等托管命令执行环境可能会在命令结束时回收其派生的后台进程，导致脚本内健康检查通过、脚本退出后端口立刻不可用。

**使用方式**:
```bash
# 重启指定模块并保持命令前台运行
bash scripts/dev/keepalive.sh restart -orchestrator

# 启动指定模块并保持命令前台运行
bash scripts/dev/keepalive.sh start -system

# 全量重启并保持命令前台运行
bash scripts/dev/keepalive.sh restart -all
```

**退出行为**: 按 `Ctrl+C` 或终止该命令时，`keepalive.sh` 会调用 `scripts/dev/stop.sh` 清理开发服务。

**适用边界**: 普通本地终端仍优先使用 `start.sh` / `restart.sh`；`keepalive.sh` 只用于命令会话结束后会回收后台进程的托管执行环境。

### modtidy.sh

**功能**: 清理所有 Go 模块的依赖

**执行模块**:
- common
- system/backend
- manager/backend
- meta/backend
- transfer/backend
- orchestrator/backend
- develop/backend
- gateway

**何时使用**:
- 切换 Git 分支后
- 拉取最新代码后
- 添加/删除 Go 依赖后
- 解决 `go.mod` 冲突后

**幂等性**: 可多次执行,不会破坏依赖

## 启动依赖关系

```
基础设施(PostgreSQL, Redis, MinIO, Meilisearch)
  ↓
System Backend (8180) - 配置中心,认证服务
  ↓
Manager Backend (8081) + Meta Backend (8082) (并行启动)
  ↓
Transfer Backend (8083) + Transfer Worker + Meta Worker + Manager Worker
  ↓
Orchestrator Backend (8084)
  ↓
Gateway (8000) - API 路由
  ↓
前端服务(Console, System, Manager, Meta, Transfer, Orchestrator)
```

**关键依赖**:
- 所有后端服务依赖**基础设施**
- Manager/Meta/Transfer 依赖 **System**(配置中心)
- Gateway 依赖**所有后端服务**
- 前端服务依赖**对应后端服务**

## 默认管理员账户

ADDP 在首次启动时会创建默认管理员账户(仅开发环境)。

### 超级管理员(总是创建)

- **用户名**: `SuperAdmin`
- **密码**: `20251001#SuperAdmin`
- **类型**: `super_admin`(跨租户管理)
- **用途**: 管理租户、查看系统日志

**环境变量自定义**:
```bash
# .env
SUPER_ADMIN_USERNAME=MyAdmin
SUPER_ADMIN_PASSWORD=MySecurePassword
SUPER_ADMIN_EMAIL=admin@company.com
```

### 默认租户管理员(可选创建)

- **用户名**: `admin`
- **密码**: `123456`
- **租户**: `默认租户`
- **类型**: `tenant_admin`(仅管理默认租户)
- **用途**: 日常开发调试

**启用方法**:
```bash
# .env
ENABLE_DEFAULT_TENANT=true

# 可选: 自定义账户信息
DEFAULT_TENANT_NAME=我的租户
DEFAULT_ADMIN_USERNAME=myadmin
DEFAULT_ADMIN_PASSWORD=MyPassword123
DEFAULT_ADMIN_EMAIL=admin@test.com
```

**安全提示**:
- ⚠️ 默认禁用(避免弱密码)
- ⚠️ 生产环境强制禁止(`ENV=production` 时跳过)
- ⚠️ 仅用于开发和演示
- 💡 生产环境应通过 UI 手动创建租户和管理员

**初始化位置**: `system/backend/internal/repository/database.go`

## 故障排查

### 服务启动失败

```bash
# 1. 查看日志
tail -f logs/system-backend.log

# 2. 检查基础设施
bash scripts/infra/status.sh

# 3. 检查端口占用
lsof -i :8180  # System backend
lsof -i :5433  # PostgreSQL

# 4. 手动重启单个服务
cd system/backend
go run cmd/server/main.go
```

### 健康检查超时

```bash
# 1. 确认服务实际启动
ps aux | grep "go run"

# 2. 手动测试健康检查
curl http://localhost:8180/health

# 3. 查看服务日志
cat logs/system-backend.log
```

### 前端启动失败

```bash
# 1. 检查 Node.js 版本(需要 16+)
node -v

# 2. 重新安装依赖
cd system/frontend
rm -rf node_modules
npm install

# 3. 清理 Vite 缓存
rm -rf node_modules/.vite
npm run dev
```

### PID 文件残留

```bash
# 如果 stop.sh 异常退出,可能残留 PID 文件
# 手动清理
rm -rf .dev-pids/
pkill -f "go run"
```

## 开发工作流

### 日常开发

```bash
# 1. 启动开发环境
bash scripts/dev/start.sh

# 2. 修改代码
vim system/backend/internal/service/user_service.go

# 3. 重启服务(自动重新编译)
bash scripts/dev/restart.sh

# 4. 查看日志
tail -f logs/system-backend.log

# 5. 停止环境
bash scripts/dev/stop.sh
```

### 添加新依赖

```bash
# 1. 添加依赖
cd system/backend
go get github.com/some/package

# 2. 清理依赖
bash scripts/dev/modtidy.sh

# 3. 重启服务
bash scripts/dev/restart.sh
```

### 切换分支

```bash
# 1. 停止服务
bash scripts/dev/stop.sh

# 2. 切换分支
git checkout feature/new-feature

# 3. 更新依赖
bash scripts/dev/modtidy.sh

# 4. 重新启动
bash scripts/dev/start.sh
```

## 已废弃的脚本

- **run.sh** (已删除): 早期轻量级启动脚本,已被 `start.sh` 替代
  - 原因: 缺少健康检查、日志管理、PID 管理
  - 替代方案: 使用 `start.sh` + `tail -f logs/*.log` 查看日志

## 相关文档

- [scripts/infra/README.md](../infra/README.md) - 基础设施脚本文档
- [CLAUDE.md](../../CLAUDE.md) - 项目整体架构
- [docs/STARTUP_ORDER.md](../../docs/STARTUP_ORDER.md) - 服务启动顺序详解
