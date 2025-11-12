# restart.sh 使用指南

## 概述

`restart.sh` 是 MVT 项目的一键启动/重启脚本，提供智能化的服务管理功能。

## 优化特性

### ✅ 自动端口清理
- 启动前自动检测端口占用（8090、5180）
- 自动终止占用进程，避免绑定失败

### ✅ 依赖自动检测
- 前端启动前强制检查 `node_modules`
- 检测关键依赖（vite）是否存在
- 缺失时自动运行 `npm install`

### ✅ 健康检查增强
- 后端：HTTP 健康检查 (`/health`)
- 前端：日志关键字检测 + 端口监听检查
- 进程存活检查（避免僵尸进程）

### ✅ 错误提示优化
- 启动失败时自动显示日志（最后 20 行）
- 区分"进程不存在"和"健康检查失败"
- 超时保护（后端 15 秒，前端 15 秒）

### ✅ 状态查询精确化
- 实时检查进程状态 + 健康状态
- 双重验证（进程存在 + 端口监听）
- 返回状态码（用于脚本集成）

## 使用方法

### 启动所有服务（推荐）

```bash
./restart.sh start
# 或直接
./restart.sh
```

**执行流程**：
1. ✓ 检查系统依赖（Go、Node.js、npm）
2. ✓ 检查配置文件（app.yaml）
3. ✓ 停止旧服务（如果存在）
4. ✓ 清理端口占用
5. ✓ 启动后端（端口 8090）
6. ✓ 等待后端健康检查通过
7. ✓ 检查前端依赖
8. ✓ 启动前端（端口 5180）
9. ✓ 显示服务状态

### 停止所有服务

```bash
./restart.sh stop
```

- 优雅停止（`kill`），2 秒后强制终止（`kill -9`）
- 清理 PID 文件

### 重启所有服务

```bash
./restart.sh restart
```

等同于 `stop` + `start`，中间间隔 2 秒。

### 查看服务状态

```bash
./restart.sh status
```

**输出示例**：
```
[SUCCESS] 后端服务: 运行中 (PID: 43807) ✓
  - API: http://localhost:8090/api/datasources
  - 健康检查: http://localhost:8090/health
[SUCCESS] 前端服务: 运行中 (PID: 43836) ✓
  - 访问地址: http://localhost:5180
```

### 查看实时日志

```bash
./restart.sh logs
```

同时监控后端和前端日志（`Ctrl+C` 退出）。

### 帮助信息

```bash
./restart.sh help
```

## 常见问题

### Q1: 端口被占用怎么办？

**A**: 脚本会自动清理！如果自动清理失败，手动清理：

```bash
# 清理后端端口
lsof -ti:8090 | xargs kill -9

# 清理前端端口
lsof -ti:5180 | xargs kill -9
```

### Q2: 健康检查超时

**A**: 查看日志排查问题：

```bash
tail -f logs/backend.log
tail -f logs/frontend.log
```

常见原因：
- 数据库/Redis 连接失败
- 配置文件错误
- 依赖缺失

### Q3: 前端依赖缺失

**A**: 脚本已自动检测并安装！如果仍有问题：

```bash
cd frontend
rm -rf node_modules package-lock.json
npm install
```

### Q4: 后端启动失败

**A**: 检查配置：

```bash
# 检查数据库连接
docker-compose ps

# 确保 PostGIS 和 Redis 已启动
docker-compose up -d

# 检查配置文件
cat backend/config/app.yaml
```

## 脚本退出码

- `0`: 成功
- `1`: 失败（依赖缺失、启动失败、健康检查超时等）

可用于 CI/CD 集成：

```bash
./restart.sh start && echo "服务启动成功" || echo "服务启动失败"
```

## 日志文件

所有日志存储在 `logs/` 目录：

- `logs/backend.log` - 后端日志
- `logs/frontend.log` - 前端日志

**查看实时日志**：
```bash
tail -f logs/backend.log
tail -f logs/frontend.log
```

## 技术细节

### 端口检查原理

```bash
lsof -Pi :8090 -sTCP:LISTEN -t
```

- `-P`: 禁用端口名称解析（加速）
- `-i :8090`: 检查 8090 端口
- `-sTCP:LISTEN`: 仅 LISTEN 状态
- `-t`: 仅输出 PID

### 健康检查机制

**后端**：
```bash
curl -s http://localhost:8090/health
```

**前端**：
```bash
grep -q "Local:.*5180" logs/frontend.log
```

检查 Vite 启动完成标记。

### 依赖检查逻辑

```bash
# 检查 node_modules 是否存在
[ -d "node_modules" ]

# 检查关键依赖
npm ls vite
```

## 与 Makefile 的关系

`restart.sh` 提供更强大的服务管理，而 Makefile 适合开发操作：

| 功能 | restart.sh | Makefile |
|------|------------|----------|
| 一键启动 | ✅ 推荐 | `make dev` |
| 端口检查 | ✅ 自动 | ❌ 手动 |
| 依赖检查 | ✅ 自动 | ❌ 需 `make init` |
| 健康检查 | ✅ 自动 | ❌ 无 |
| 状态查询 | ✅ 详细 | ❌ 无 |
| 日志查看 | ✅ 集成 | `make logs` |

**推荐使用场景**：
- **开发环境**：`./restart.sh start`
- **生产部署**：`make build` + Docker
- **快速测试**：`make dev-backend` / `make dev-frontend`

## 优化历史

**v1.0**（初始版本）：
- 基础启动/停止功能

**v2.0**（优化版，2025-11-11）：
- ✅ 自动端口清理
- ✅ 前端依赖强制检查
- ✅ 进程存活检查
- ✅ 健康检查超时保护
- ✅ 启动失败时显示日志
- ✅ 状态查询双重验证
- ✅ 移除 `set -e`（允许部分失败）

---

**维护者**: ADDP Labs
**最后更新**: 2025-11-11
