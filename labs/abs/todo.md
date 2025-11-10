# ABS 配置统一规范 - 执行总结

## 已完成的工作

根据您的要求，我已经完成了对 ABS 项目目录、端口、URL入口点等的统一规范化。所有变更都已实施并记录。

## 核心变更

### 1. 路径统一使用相对路径

**原则**: 所有路径相对于 `abs/` 根目录，不使用 `./` 前缀

**已修改文件**:
- `.env` - 环境配置
- `.env.example` - 配置模板

**变更内容**:
```bash
# 修改前
WORKSPACE_DIR=./workspace
APPS_DATA_FILE=./workspace/apps.json

# 修改后
WORKSPACE_DIR=workspace              # 相对于abs根目录
APPS_DATA_FILE=workspace/apps.json   # 相对于abs根目录
```

### 2. 应用工作区统一目录结构

**位置**: `abs/workspace/`

**子目录命名**: UUID格式，例如:
```
workspace/
├── apps.json
├── 9dc454cf-fc02-4042-8899-e02b6e421902/
└── ec50e772-2daa-4495-8dcd-58aadf147ecf/
```

### 3. 应用入口URL统一格式

**格式**: `http://localhost:5180?app=<app-id>`

**已修改**:
- `workspace/apps.json` - 更新了所有现有应用的 entry_url
- `backend/internal/service/task_service.go` - 新应用自动使用此格式

**示例**:
```
http://localhost:5180?app=9dc454cf-fc02-4042-8899-e02b6e421902
http://localhost:5180?app=ec50e772-2daa-4495-8dcd-58aadf147ecf
```

### 4. workspace_path 使用统一相对路径

**格式**: `<app-id>`（不包含 `workspace/` 前缀）

**已修改**:
- `workspace/apps.json` - 所有应用的 workspace_path
- `backend/internal/service/task_service.go:584` - 新应用注册时自动使用

**示例**:
```json
{
  "workspace_path": "9dc454cf-fc02-4042-8899-e02b6e421902"
}
```

### 5. 脚本和Makefile验证

**验证结果**:
- `Makefile` ✅ 已符合规范，无需修改
- `restart.sh` ✅ 已符合规范，路径处理正确
- `start.sh` ✅ 已符合规范，从abs/根目录执行

所有脚本都正确地从 `abs/` 根目录执行操作。

### 6. 文档更新

**已更新的文档**:
1. `QUICKSTART.md` - 更新路径说明和示例
2. `readme.md` - 更新配置示例、路径说明、访问格式

**新增的文档**:
1. `STANDARDS.md` - 完整的开发规范文档（8.1KB）
2. `CHANGES_SUMMARY.md` - 详细变更总结（8.5KB）
3. `QUICK_REFERENCE.md` - 快速参考卡（1.4KB）
4. `todo.md` - 本执行总结

## 端口分配

| 服务 | 端口 | 说明 |
|------|------|------|
| 前端 | 5180 | Vue.js开发服务器 |
| 后端 | 8090 | Go HTTP服务器 |
| 应用 | 8091+ | 动态分配给需要独立端口的应用 |

## 修改的文件清单

### 配置文件
- [x] `.env` - 路径格式统一
- [x] `.env.example` - 路径格式统一

### 数据文件
- [x] `workspace/apps.json` - 更新所有应用的entry_url和workspace_path

### 代码文件
- [x] `backend/internal/service/task_service.go` - WorkspacePath使用完整相对路径

### 文档文件
- [x] `QUICKSTART.md` - 更新路径和URL格式
- [x] `readme.md` - 更新配置示例和说明
- [x] `STANDARDS.md` - 新增完整规范文档
- [x] `CHANGES_SUMMARY.md` - 新增变更总结
- [x] `QUICK_REFERENCE.md` - 新增快速参考
- [x] `todo.md` - 本执行总结

### 验证无需修改的文件
- [x] `Makefile` - 已验证符合规范
- [x] `restart.sh` - 已验证符合规范
- [x] `start.sh` - 已验证符合规范

## 验证测试

```bash
# 1. 检查.env配置
cat .env | grep -E "WORKSPACE_DIR|APPS_DATA_FILE"
# 输出:
# WORKSPACE_DIR=workspace
# APPS_DATA_FILE=workspace/apps.json

# 2. 检查apps.json格式
head -15 workspace/apps.json
# 确认 entry_url 和 workspace_path 格式正确
```

## 使用方式

### 启动系统

```bash
# 从abs根目录执行
cd abs
./restart.sh

# 或使用Makefile
make dev
```

### 访问应用

```
# 访问前端
http://localhost:5180

# 访问特定应用
http://localhost:5180?app=9dc454cf-fc02-4042-8899-e02b6e421902
```

## 规范要点

1. **所有路径相对于abs/根目录**
   - ✅ `workspace/<app-id>`
   - ❌ `./workspace/<app-id>`
   - ❌ `/absolute/path/`

2. **配置文件位置**
   - ✅ `abs/.env`
   - ❌ `abs/backend/.env`

3. **应用工作区**
   - 位置: `abs/workspace/`
   - 子目录: `<app-id>/` (UUID)
   - 备份: `<app-id>.backup.YYYYMMdd-HHmmss/`

4. **应用入口URL**
   - 格式: `http://localhost:5180?app=<app-id>`
   - 端口: 固定5180

5. **脚本执行**
   - 始终从 `abs/` 根目录执行
   - 使用 `./restart.sh` 或 `make dev`

## 后续注意事项

### 开发新功能时

1. 路径配置使用相对于abs/的格式，不用`./`前缀
2. 应用注册使用 `filepath.Join("workspace", appID)`
3. URL生成使用 `fmt.Sprintf("%s?app=%s", frontend, appID)`
4. 从abs/根目录执行脚本和命令

### 提交代码前检查

- [ ] 路径相对于 `abs/` 根目录
- [ ] 没有使用 `./` 前缀
- [ ] entry_url 使用 `?app=<id>` 格式
- [ ] workspace_path 使用 `<app-id>`（不包含 `workspace/` 前缀）
- [ ] .env 在 abs/ 根目录
- [ ] 端口使用标准分配
- [ ] 文档已同步更新

## 参考文档

| 文档 | 用途 | 大小 |
|------|------|------|
| `STANDARDS.md` | 完整开发规范 | 8.1KB |
| `CHANGES_SUMMARY.md` | 详细变更总结 | 8.5KB |
| `QUICK_REFERENCE.md` | 快速参考卡 | 1.4KB |
| `todo.md` | 本执行总结 | - |

## 总结

✅ **已完成的任务**:
1. 统一所有路径使用相对路径（相对于abs/根目录）
2. .env 和 .env.example 放在abs根目录，路径配置不用`./`前缀
3. 所有操作从abs根目录开始
4. 新建应用位置统一为 workspace/<app-id>/
5. 应用入口URL统一为 http://localhost:5180?app=<app-id>
6. 修改现有两个应用符合新规范
7. 检查并验证所有sh、Makefile等文件
8. 创建完整的规范文档防止后续乱改

所有要求都已完成并记录在案！

---

**完成时间**: 2025-11-10
**维护者**: ADDP Development Team

---

## 待改进的架构问题

### 问题：应用为什么需要独立端口？

**当前架构**：
```
前端 (5180) → iframe → Python应用 (8091独立端口)
```

**问题分析**：
1. **用户体验差** - 用户需要记住多个端口
2. **端口冲突风险** - 多个应用可能抢占同一端口
3. **进程管理复杂** - 需要跟踪每个应用的 PID
4. **CORS 问题** - 跨域请求可能受限

**根本原因**：
- AI 生成的代码默认创建独立的 HTTP 服务器
- 应用混合了静态文件和动态 API（如 `/api/data`）
- 当前 ABS Backend 只能服务静态文件，不能代理动态请求

**推荐改进方案：反向代理模式**

让 ABS Backend 作为统一入口：

```
用户访问: http://localhost:5180/?app=xxx
  ↓
前端 iframe: /api/app-proxy/xxx/
  ↓
ABS Backend (8090) 检测应用类型
  ↓ 静态文件 → 直接返回
  ↓ 动态请求 → 代理到应用端口
Python应用 (内部端口，用户无感知)
```

**实现步骤**：
1. 在 `backend/internal/api/router.go` 添加 `/api/app-proxy/:app_id/*path` 路由
2. 新建 `backend/internal/api/proxy.go` 实现反向代理逻辑
3. 代理逻辑：
   - 根据 `app_id` 查找应用配置和运行端口
   - 将请求转发到 `localhost:<app_port>`
   - 返回响应给前端
4. 更新前端 `AppPreview.vue`：
   - iframe src 改为 `/api/app-proxy/<app-id>/`
   - 所有请求通过代理访问

**优点**：
- ✅ 统一端口访问（5180）
- ✅ 自动处理 CORS
- ✅ 统一的日志和访问控制
- ✅ 应用端口可动态分配（用户无感知）
- ✅ 支持静态文件 + 动态 API 混合应用

**参考实现**：
```go
// backend/internal/api/proxy.go
func ProxyToApp(appService *service.AppService) gin.HandlerFunc {
    return func(c *gin.Context) {
        appID := c.Param("app_id")
        path := c.Param("path")

        // 1. 查找应用和端口
        app, err := appService.GetApp(appID)
        if err != nil {
            c.String(404, "App not found")
            return
        }

        // 2. 解析应用端口（从 start_command 或配置）
        port := extractPort(app.StartCommand)

        // 3. 反向代理请求
        targetURL := fmt.Sprintf("http://localhost:%d%s", port, path)
        proxy := httputil.NewSingleHostReverseProxy(...)
        proxy.ServeHTTP(c.Writer, c.Request)
    }
}
```

**优先级**: 中 - 当前架构可用，但此改进会显著提升用户体验

---