# ABS 规范快速参考卡

## 目录和路径

```bash
# 工作目录：始终从 abs/ 根目录开始
cd abs

# 相对路径格式（不使用 ./ 前缀）
WORKSPACE_DIR=workspace
APPS_DATA_FILE=workspace/apps.json

# 应用目录
workspace/<app-id>/          # ✅ 正确
workspace/9dc454cf-fc02-4042-8899-e02b6e421902/

./workspace/<app-id>/        # ❌ 错误（不用./）
backend/workspace/           # ❌ 错误（从abs/开始）
```

## 端口

| 服务 | 端口 |
|------|------|
| 前端 | 5180 |
| 后端 | 8090 |
| 应用 | 8091+ |

## URL格式

```
# 应用入口URL（统一格式）
http://localhost:5180?app=<app-id>

# 示例
http://localhost:5180?app=9dc454cf-fc02-4042-8899-e02b6e421902
```

## apps.json格式

```json
{
  "id": "<app-id>",
  "entry_url": "http://localhost:5180?app=<app-id>",
  "workspace_path": "<app-id>"
}
```

## 常用命令

```bash
# 启动（从abs/根目录）
./restart.sh
# 或
make dev

# 停止
make stop

# 清理
make clean
```

## 代码规范

```go
// 路径拼接
filepath.Join("workspace", task.ID)         // ✅
filepath.Join(s.config.WorkspaceDir, id)    // ✅

"./workspace/" + task.ID                     // ❌
```

## 检查清单

- [ ] 路径相对于 `abs/` 根目录
- [ ] 不使用 `./` 前缀
- [ ] entry_url 使用 `?app=<id>` 格式
- [ ] workspace_path 使用 `<app-id>`（不包含 `workspace/` 前缀）
- [ ] 脚本从 `abs/` 执行
- [ ] .env 在 `abs/` 根目录

---

详见 [STANDARDS.md](STANDARDS.md)
