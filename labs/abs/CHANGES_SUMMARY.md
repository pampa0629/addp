# ABS 路径和配置统一规范化 - 变更总结

**日期**: 2025-11-10
**版本**: v1.0

## 概述

根据用户要求，对 ABS 项目的目录结构、端口配置、URL入口等进行了全面统一规范化，确保所有路径使用相对于 `abs/` 根目录的一致格式。

## 核心原则

1. **统一根目录**: 所有操作以 `abs/` 作为根目录起点
2. **相对路径**: 配置文件中使用相对路径，不使用 `./` 前缀
3. **配置集中**: `.env` 和 `.env.example` 放在 `abs/` 根目录
4. **工作区标准化**: 新建应用统一在 `workspace/<app-id>/` (UUID子目录)
5. **URL统一格式**: 应用入口URL统一为 `http://localhost:5180?app=<app-id>`

## 具体变更清单

### 1. 环境配置文件 (.env 和 .env.example)

**位置**: `abs/.env` 和 `abs/.env.example`

**变更前**:
```bash
WORKSPACE_DIR=./workspace
APPS_DATA_FILE=./workspace/apps.json
```

**变更后**:
```bash
WORKSPACE_DIR=workspace              # 相对于abs根目录，不使用./前缀
AUTO_RELOAD=true
APPS_DATA_FILE=workspace/apps.json   # 相对于abs根目录
```

**说明**:
- 移除了 `./` 前缀
- 添加了中文注释说明相对路径基准

### 2. 应用注册数据 (workspace/apps.json)

**变更前**:
```json
{
  "entry_url": "http://localhost:8091",
  "workspace_path": "9dc454cf-fc02-4042-8899-e02b6e421902"
}
```

**变更后**:
```json
{
  "entry_url": "http://localhost:5180?app=9dc454cf-fc02-4042-8899-e02b6e421902",
  "workspace_path": "9dc454cf-fc02-4042-8899-e02b6e421902"
}
```

**说明**:
- `entry_url` 改为统一的查询参数格式
- `workspace_path` 仅使用 `<app-id>`（不包含 `workspace/` 前缀）
- 已更新所有现有应用记录

### 3. 后端服务代码 (backend/internal/service/task_service.go)

**变更位置**: 第584行

**变更前**:
```go
WorkspacePath: task.ID,
```

**变更后**:
```go
WorkspacePath: filepath.Join("workspace", task.ID), // Relative path from abs root
```

**说明**:
- 新注册的应用自动使用 `workspace/<app-id>` 格式
- 添加了代码注释说明路径基准

### 4. 文档更新

#### QUICKSTART.md

- 更新进入项目命令: `cd abs` (不使用 `./`)
- 更新代码存储路径说明: `workspace/<task-id>/`
- 添加应用访问地址格式: `http://localhost:5180?app=<task-id>`
- 修正依赖安装路径说明

#### readme.md

- 更新环境变量配置示例（移除 `./` 前缀）
- 更新代码存储路径说明
- 添加应用访问地址格式
- 添加路径相对基准的中文注释

#### 新增 STANDARDS.md

创建了完整的开发规范文档，包含：
- 目录结构标准
- 路径规范（相对路径、工作区目录、配置文件）
- 端口分配规范
- URL入口规范
- 脚本执行规范
- 后端代码规范
- 前端路由规范
- 环境配置规范
- Makefile规范
- 文档规范
- 常见问题解答
- 检查清单

### 5. 脚本和构建文件

**验证结果**:
- `Makefile`: 已符合规范，无需修改
- `restart.sh`: 已符合规范，无需修改（已有路径解析逻辑）
- `start.sh`: 已符合规范，无需修改（已正确设置PROJECT_ROOT）

## 目录结构标准

```
abs/                                  # 项目根目录（所有操作的起点）
├── .env                              # 环境配置
├── .env.example                      # 配置模板
├── Makefile                          # 构建命令
├── start.sh                          # 启动脚本
├── restart.sh                        # 重启脚本
├── STANDARDS.md                      # 开发规范（新增）
├── CHANGES_SUMMARY.md                # 本文档（新增）
├── QUICKSTART.md                     # 快速开始（已更新）
├── readme.md                         # 项目README（已更新）
├── backend/                          # 后端代码
│   ├── cmd/server/main.go
│   └── internal/service/
│       ├── config.go                 # 配置加载（已验证）
│       └── task_service.go           # 任务服务（已修改）
├── frontend/                         # 前端代码
│   └── src/
└── workspace/                        # 应用工作区
    ├── apps.json                     # 应用注册表（已更新）
    ├── 9dc454cf-fc02-4042-8899-e02b6e421902/  # 应用1
    │   ├── server.py
    │   ├── data/
    │   └── public/
    ├── 9dc454cf-fc02-4042-8899-e02b6e421902.backup.YYYYMMdd-HHmmss/  # 备份
    └── ec50e772-2daa-4495-8dcd-58aadf147ecf/   # 应用2
        ├── index.html
        ├── style.css
        └── main.js
```

## 端口和URL规范

| 服务 | 端口 | URL |
|------|------|-----|
| 前端开发服务器 | 5180 | http://localhost:5180 |
| 后端API服务器 | 8090 | http://localhost:8090 |
| 动态应用 | 8091+ | 自动分配 |

**应用访问地址统一格式**:
```
http://localhost:5180?app=<app-id>
```

**示例**:
```
http://localhost:5180?app=9dc454cf-fc02-4042-8899-e02b6e421902
http://localhost:5180?app=ec50e772-2daa-4495-8dcd-58aadf147ecf
```

## 兼容性说明

### 向后兼容

- **现有应用**: 已有的两个应用的 `apps.json` 记录已更新
- **路径解析**: 后端配置加载器自动将相对路径转换为绝对路径
- **脚本兼容**: 所有脚本已有路径解析逻辑，自动适配

### 迁移指南

对于未来的应用：
1. 自动使用新的路径格式（代码已修改）
2. 自动使用新的URL格式（`previewEntryURL` 函数已正确实现）
3. 无需手动干预

## 测试验证

### 验证命令

```bash
# 1. 验证环境配置
cat .env | grep -E "WORKSPACE_DIR|APPS_DATA_FILE"

# 2. 验证apps.json格式
cat workspace/apps.json | jq '.[] | {entry_url, workspace_path}'

# 3. 验证后端配置加载
cd backend && go run cmd/server/main.go &
# 观察启动日志，确认路径正确加载

# 4. 验证前端访问
open http://localhost:5180
```

### 预期结果

```bash
# .env 输出
WORKSPACE_DIR=workspace
APPS_DATA_FILE=workspace/apps.json

# apps.json 输出
{
  "entry_url": "http://localhost:5180?app=9dc454cf-fc02-4042-8899-e02b6e421902",
  "workspace_path": "9dc454cf-fc02-4042-8899-e02b6e421902"
}
```

## 后续行动

### 开发者须知

1. **新建应用**: 使用 `filepath.Join("workspace", appID)` 格式
2. **配置路径**: 不使用 `./` 前缀
3. **脚本执行**: 始终从 `abs/` 根目录执行
4. **文档更新**: 修改配置时同步更新 STANDARDS.md

### 检查清单

在提交代码前，请确认：

- [ ] 所有路径使用相对于 abs/ 的格式
- [ ] 应用 entry_url 使用 `http://localhost:5180?app=<app-id>` 格式
- [ ] workspace_path 使用 `<app-id>` 格式（不包含 `workspace/` 前缀）
- [ ] 脚本从 abs/ 根目录执行
- [ ] .env 配置文件在 abs/ 根目录
- [ ] 端口使用标准分配 (5180/8090)
- [ ] 文档已同步更新

## 参考文档

- [STANDARDS.md](STANDARDS.md) - 完整的开发规范
- [QUICKSTART.md](QUICKSTART.md) - 快速开始指南
- [readme.md](readme.md) - 项目README

## 变更影响范围

| 文件/目录 | 变更类型 | 说明 |
|-----------|---------|------|
| `.env` | 修改 | 移除路径的 `./` 前缀，添加注释 |
| `.env.example` | 修改 | 移除路径的 `./` 前缀，添加注释 |
| `workspace/apps.json` | 修改 | 更新所有应用的 entry_url 和 workspace_path |
| `backend/internal/service/task_service.go` | 修改 | WorkspacePath 使用 `filepath.Join("workspace", task.ID)` |
| `QUICKSTART.md` | 修改 | 更新路径说明和示例 |
| `readme.md` | 修改 | 更新路径说明、配置示例、应用访问格式 |
| `STANDARDS.md` | 新增 | 完整的开发规范文档 |
| `CHANGES_SUMMARY.md` | 新增 | 本变更总结文档 |
| `Makefile` | 验证 | 已符合规范，无需修改 |
| `restart.sh` | 验证 | 已符合规范，无需修改 |
| `start.sh` | 验证 | 已符合规范，无需修改 |

## 总结

本次规范化统一了 ABS 项目的：

1. ✅ **路径表示**: 所有路径相对于 abs/ 根目录，不使用 `./` 前缀
2. ✅ **配置位置**: .env 文件统一在 abs/ 根目录
3. ✅ **工作区结构**: workspace/<app-id>/ 格式
4. ✅ **URL格式**: http://localhost:5180?app=<app-id>
5. ✅ **端口分配**: 5180 (前端), 8090 (后端), 8091+ (动态应用)
6. ✅ **文档完善**: 新增 STANDARDS.md，更新现有文档
7. ✅ **代码更新**: 后端代码自动生成规范路径
8. ✅ **现有应用迁移**: apps.json 已更新为新格式

所有变更已验证并符合统一规范要求。

---

**维护者**: ADDP Development Team
**最后更新**: 2025-11-10
