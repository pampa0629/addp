# ABS 开发规范 (Development Standards)

本文档定义了 ABS (AI Bootstrapping System) 项目的开发规范，确保路径、端口、URL等配置的一致性。

## 目录结构标准

### 根目录 (Root Directory)

所有操作都应以 `abs/` 作为根目录起点进行：

```
abs/
├── .env                      # 环境配置（从abs/根目录）
├── .env.example              # 环境配置模板
├── Makefile                  # 构建命令（从abs/根目录执行）
├── start.sh                  # 启动脚本（从abs/根目录执行）
├── restart.sh                # 重启脚本（从abs/根目录执行）
├── STANDARDS.md              # 本文档
├── backend/                  # 后端代码
│   ├── cmd/server/main.go
│   └── internal/
└── frontend/                 # 前端代码
    └── src/
└── workspace/                # 应用工作区（相对于abs/）
    ├── apps.json             # 应用注册表
    └── <app-id>/             # 各应用目录（UUID命名）
```

## 路径规范

### 1. 使用相对路径

**原则**: 所有配置文件、脚本、代码中的路径都使用相对于 `abs/` 根目录的路径。

**示例**:
```bash
# ✅ 正确
WORKSPACE_DIR=workspace
APPS_DATA_FILE=workspace/apps.json

# ❌ 错误
WORKSPACE_DIR=./workspace
WORKSPACE_DIR=/Users/xxx/abs/workspace
```

### 2. 工作区目录规范

**位置**: `abs/workspace/`

**子目录命名**: 以应用 ID (UUID) 命名，例如:
```
workspace/
├── apps.json
├── 9dc454cf-fc02-4042-8899-e02b6e421902/
│   ├── server.py
│   ├── data/
│   └── public/
└── ec50e772-2daa-4495-8dcd-58aadf147ecf/
    ├── index.html
    ├── style.css
    └── main.js
```

**备份目录**: 应用修改时创建备份，命名格式为 `<app-id>.backup.YYYYMMdd-HHmmss`

### 3. 配置文件路径

**位置**: 配置文件统一放在 `abs/` 根目录

```bash
abs/.env           # 主配置文件
abs/.env.example   # 配置模板
```

**不允许**:
- ❌ `backend/.env`
- ❌ `frontend/.env`
- ❌ 任何子目录中的独立.env文件

## 端口分配规范

| 服务 | 端口 | 说明 |
|------|------|------|
| 前端 (Frontend) | 5180 | Vue.js 开发服务器 |
| 后端 (Backend) | 8090 | Go HTTP 服务器 |
| 动态应用端口 | 8091+ | 自动分配给需要独立端口的应用 |

**端口分配策略**:
- 静态 HTML 应用: 不占用独立端口，通过前端路由访问
- 服务型应用 (Python/Node): 自动分配空闲端口 (8091, 8092, ...)

## URL 入口规范

### 应用中心 (Application Center) Entry URL

**统一格式**: `http://localhost:5180?app=<app-id>`

**示例**:
```
http://localhost:5180?app=9dc454cf-fc02-4042-8899-e02b6e421902
http://localhost:5180?app=ec50e772-2daa-4495-8dcd-58aadf147ecf
```

**实现位置**:
- 后端生成: `backend/internal/service/task_service.go:711` (`previewEntryURL` 函数)
- 前端路由: 前端根据 `?app=` 参数加载对应应用

**apps.json 格式**:
```json
{
  "id": "9dc454cf-fc02-4042-8899-e02b6e421902",
  "entry_url": "http://localhost:5180?app=9dc454cf-fc02-4042-8899-e02b6e421902",
  "workspace_path": "9dc454cf-fc02-4042-8899-e02b6e421902"
}
```

### 服务型应用独立端口

对于需要独立端口的应用 (如 Python 服务器)，入口 URL 仍统一使用应用中心链接：
```json
{
  "entry_url": "http://localhost:5180?app=<app-id>",
  "start_command": ["python3", "server.py", "--port", "8091"]
}
```

## 脚本执行规范

### 启动顺序

所有脚本必须从 `abs/` 根目录执行：

```bash
cd /path/to/addp/labs/abs  # 进入abs根目录
./start.sh                  # 启动
./restart.sh                # 重启
make dev                    # 或使用Makefile
```

### 环境变量加载

脚本执行顺序：
1. 切换到 PROJECT_ROOT (abs/)
2. 加载 `.env` 文件
3. 解析相对路径为绝对路径
4. 启动后端和前端

**restart.sh 示例**:
```bash
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$PROJECT_ROOT"
source .env
# 路径自动解析为绝对路径
RESOLVED_WORKSPACE_DIR=$(resolve_dir_path "$WORKSPACE_DIR")
```

## 后端代码规范

### 路径处理

**使用 `filepath.Join`**: 所有路径拼接使用 `filepath.Join`

```go
// ✅ 正确
workspaceDir := filepath.Join(s.config.WorkspaceDir, task.ID)
manifestPath := filepath.Join(workspaceDir, "app_manifest.json")

// ❌ 错误
workspaceDir := s.config.WorkspaceDir + "/" + task.ID
```

### 应用注册

**WorkspacePath**: 存储相对于 abs/ 的路径

```go
req := &models.CreateAppRequest{
    // ...
    WorkspacePath: filepath.Join("workspace", task.ID), // "workspace/<uuid>"
    EntryURL:      fmt.Sprintf("%s?app=%s", frontend, task.ID),
    // ...
}
```

### 配置加载

**config.go**: 自动将相对路径转换为绝对路径

```go
workspaceDir := getEnv("WORKSPACE_DIR", "workspace")
absWorkspaceDir, _ := filepath.Abs(workspaceDir)  // 转换为绝对路径
```

## 前端路由规范

### 应用预览路由

前端应实现基于查询参数的应用加载：

```javascript
// 解析 URL: http://localhost:5180?app=<app-id>
const urlParams = new URLSearchParams(window.location.search);
const appId = urlParams.get('app');

// 根据 appId 加载应用内容
if (appId) {
    loadAppPreview(appId);
}
```

## 环境配置规范

### .env 文件结构

```bash
# Code Generator Selection
CODE_GENERATOR=codex_cli

# Codex CLI Configuration
CODEX_CLI_PATH=codex
CODEX_CLI_ARGS="--skip-git-repo-check --full-auto"
CODEX_CLI_TIMEOUT=300s

# Server Configuration
PORT=8090
FRONTEND_URL=http://localhost:5180

# Code Execution (所有路径相对于abs/根目录)
WORKSPACE_DIR=workspace
AUTO_RELOAD=true
APPS_DATA_FILE=workspace/apps.json
```

**关键原则**:
- 路径不使用 `./` 前缀
- 路径相对于 abs/ 根目录
- 端口固定使用 5180 (前端) 和 8090 (后端)

## Makefile 规范

### 命令定义

所有 Makefile 命令从 abs/ 根目录执行：

```makefile
dev-backend:
	@echo "Starting ABS backend..."
	set -a; [ -f .env ] && . ./.env; set +a; cd backend && go run cmd/server/main.go

dev-frontend:
	@echo "Starting ABS frontend..."
	cd frontend && npm run dev
```

**原则**:
- 使用 `cd` 进入子目录执行
- 环境变量从根目录 `.env` 加载
- 所有路径相对于根目录

## 文档规范

### 文档更新要求

当路径、端口、URL 标准发生变化时，必须同步更新：

1. `STANDARDS.md` (本文档)
2. `QUICKSTART.md` - 快速开始指南
3. `readme.md` - 项目README
4. `CLAUDE.md` - AI助手指南
5. 相关 API 文档

### 示例引用

文档中的示例必须使用符合规范的路径和URL：

```bash
# ✅ 正确的示例
cd abs
./start.sh

# 访问应用
http://localhost:5180?app=9dc454cf-fc02-4042-8899-e02b6e421902

# ❌ 错误的示例
cd abs/backend
go run cmd/server/main.go  # 单独启动不完整
```

## 常见问题

### Q: 为什么不使用 `./` 前缀？

A: `./` 是冗余的，且在某些环境下可能导致路径解析问题。直接使用相对路径 `workspace` 更简洁明确。

### Q: 如何确保路径一致性？

A:
1. 所有操作从 abs/ 根目录开始
2. 脚本使用 `PROJECT_ROOT` 变量确保工作目录正确
3. 后端使用 `filepath.Abs()` 将相对路径转换为绝对路径

### Q: 如何处理跨平台路径问题？

A: 使用 Go 的 `filepath` 包自动处理路径分隔符差异：
```go
filepath.Join("workspace", task.ID)  // 跨平台兼容
```

### Q: apps.json 为什么放在 workspace/ 下？

A:
- 逻辑归属：apps.json 记录的是工作区中的应用
- 便于备份：备份 workspace/ 即可包含所有应用数据和注册信息
- 路径一致：所有应用相关数据在同一目录

## 检查清单

在提交代码前，请确认：

- [ ] 所有路径使用相对于 abs/ 的格式
- [ ] 应用 entry_url 使用 `http://localhost:5180?app=<app-id>` 格式
- [ ] workspace_path 使用 `<app-id>` 格式（不包含 `workspace/` 前缀）
- [ ] 脚本从 abs/ 根目录执行
- [ ] .env 配置文件在 abs/ 根目录
- [ ] 端口使用标准分配 (5180/8090)
- [ ] 文档已同步更新

---

**最后更新**: 2025-11-10
**版本**: 1.0
**维护者**: ADDP Development Team
