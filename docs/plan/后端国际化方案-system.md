# 后端错误消息国际化（System Backend i18n）

## Context

Phase 2 i18n 前端和中间件部分已完成，但 system backend 的错误消息仍为硬编码中文，英文用户会看到中文错误提示。
本次在已有的 `GetLang(c)` 基础上，用 `go-i18n/v2` + TOML 文件 + `go:embed` 实现翻译，并将 handler 中的硬编码消息替换为 `i18n.T(c, key)` 调用。

**遵循就近原则**：system backend 的翻译文件放在 `system/backend/locales/`，不放 common。
common 中只放通用错误消息（如 `err.invalid_id`、`err.unauthorized`），system 业务消息放在 system 自己的 locales 下。

**依赖**：引入 `github.com/nicksnyder/go-i18n/v2`（在 `system/backend/go.mod` 中添加）。

---

## 文件结构

```
common/middleware/i18n/
├── i18n.go              (已有 - 中间件 + GetLang)
└── translator.go        (新建 - 封装 go-i18n/v2 的 Bundle + T() 函数，供各模块复用)

system/backend/
└── locales/
    ├── zh-cn.toml       (新建 - system 模块的翻译消息)
    └── en.toml          (新建)
```

---

## 涉及文件

| 文件 | 变更类型 |
|------|----------|
| `common/middleware/i18n/translator.go` | 新建 - 封装 go-i18n/v2 Bundle + T() |
| `common/go.mod` | 添加 go-i18n/v2 直接依赖 |
| `common/api/handler_helpers.go` | 修改 2 处（common 通用错误） |
| `system/backend/locales/zh-cn.toml` | 新建 - system 业务翻译 |
| `system/backend/locales/en.toml` | 新建 |
| `system/backend/go.mod` | 添加 go-i18n/v2 直接依赖 |
| `system/backend/internal/api/auth_handler.go` | 修改 6 处 |
| `system/backend/internal/api/log_handler.go` | 修改 5 处 |
| `system/backend/internal/api/module_registry_handler.go` | 修改 4 处 |
| `system/backend/internal/api/cleanup_handler.go` | 修改 2 处 |

---

## 实现步骤

### Step 1：创建 TOML 翻译文件

**`common/middleware/i18n/locales/zh-cn.toml`**
```toml
"err.invalid_id"             = "无效的ID参数"
"err.unauthorized"           = "未授权"
"err.token_gen_failed"       = "生成令牌失败"
"err.register_disabled"      = "注册功能已关闭"
"err.missing_auth_header"    = "缺少 Authorization 头"
"err.invalid_auth_format"    = "无效的 Authorization 格式"
"err.invalid_token"          = "无效的 token"
"err.token_refresh_failed"   = "生成新 token 失败"
"err.log_not_found"          = "日志不存在"
"err.export_failed"          = "导出失败"
"err.invalid_params"         = "无效的请求参数"
"err.audit_log_create_failed"= "创建审计日志失败"
"err.module_not_found"       = "模块不存在"
"msg.module_registered"      = "模块注册成功"
"msg.module_heartbeat"       = "心跳更新成功"
"msg.module_deleted"         = "模块注销成功"
"msg.audit_log_created"      = "审计日志已创建"
```

**`common/middleware/i18n/locales/en.toml`**
```toml
"err.invalid_id"             = "Invalid ID parameter"
"err.unauthorized"           = "Unauthorized"
"err.token_gen_failed"       = "Failed to generate token"
"err.register_disabled"      = "Registration is disabled"
"err.missing_auth_header"    = "Missing Authorization header"
"err.invalid_auth_format"    = "Invalid Authorization format"
"err.invalid_token"          = "Invalid token"
"err.token_refresh_failed"   = "Failed to refresh token"
"err.log_not_found"          = "Log not found"
"err.export_failed"          = "Export failed"
"err.invalid_params"         = "Invalid request parameters"
"err.audit_log_create_failed"= "Failed to create audit log"
"err.module_not_found"       = "Module not found"
"msg.module_registered"      = "Module registered successfully"
"msg.module_heartbeat"       = "Heartbeat updated successfully"
"msg.module_deleted"         = "Module unregistered successfully"
"msg.audit_log_created"      = "Audit log created"
```

### Step 2：新建 `common/middleware/i18n/translator.go`

```go
package i18n

import (
    "embed"
    "sync"

    "github.com/gin-gonic/gin"
    "github.com/pelletier/go-toml/v2"
)

//go:embed locales/*.toml
var localeFS embed.FS

// 消息 key 常量
const (
    MsgInvalidID            = "err.invalid_id"
    MsgUnauthorized         = "err.unauthorized"
    MsgTokenGenFailed       = "err.token_gen_failed"
    MsgRegisterDisabled     = "err.register_disabled"
    MsgMissingAuthHeader    = "err.missing_auth_header"
    MsgInvalidAuthFormat    = "err.invalid_auth_format"
    MsgInvalidToken         = "err.invalid_token"
    MsgTokenRefreshFailed   = "err.token_refresh_failed"
    MsgLogNotFound          = "err.log_not_found"
    MsgExportFailed         = "err.export_failed"
    MsgInvalidParams        = "err.invalid_params"
    MsgAuditLogCreateFailed = "err.audit_log_create_failed"
    MsgModuleNotFound       = "err.module_not_found"
    MsgModuleRegistered     = "msg.module_registered"
    MsgModuleHeartbeat      = "msg.module_heartbeat"
    MsgModuleDeleted        = "msg.module_deleted"
    MsgAuditLogCreated      = "msg.audit_log_created"
)

var (
    translations map[string]map[string]string
    loadOnce     sync.Once
)

func loadTranslations() {
    loadOnce.Do(func() {
        translations = make(map[string]map[string]string)
        for _, lang := range []string{LangZhCN, LangEn} {
            data, err := localeFS.ReadFile("locales/" + lang + ".toml")
            if err != nil {
                continue
            }
            var msgs map[string]string
            if err := toml.Unmarshal(data, &msgs); err != nil {
                continue
            }
            translations[lang] = msgs
        }
    })
}

// T 根据 gin context 中的语言偏好翻译消息 key。
// 若 key 不存在则 fallback 到 zh-cn，再不存在则返回 key 本身。
func T(c *gin.Context, key string) string {
    loadTranslations()
    lang := GetLang(c)
    if msgs, ok := translations[lang]; ok {
        if msg, ok := msgs[key]; ok {
            return msg
        }
    }
    if msgs, ok := translations[LangZhCN]; ok {
        if msg, ok := msgs[key]; ok {
            return msg
        }
    }
    return key
}

// TWithDetail 翻译消息 key 并追加动态详情（如错误原因）。
func TWithDetail(c *gin.Context, key, detail string) string {
    return T(c, key) + ": " + detail
}
```

### Step 3：提升 go-toml/v2 为直接依赖

在 `common/go.mod` 中将 `github.com/pelletier/go-toml/v2` 从 `// indirect` 移到 `require` 直接依赖块。

### Step 4：修改 `common/api/handler_helpers.go`

添加 import `i18nmiddleware "github.com/addp/common/middleware/i18n"`，替换：
- `"无效的ID参数"` → `i18nmiddleware.T(c, i18nmiddleware.MsgInvalidID)`
- `"未授权"` → `i18nmiddleware.T(c, i18nmiddleware.MsgUnauthorized)`

### Step 5：修改 `auth_handler.go`

添加 import `i18n "github.com/addp/common/middleware/i18n"`，替换 6 处：

| 原文 | 替换为 |
|------|--------|
| `"生成令牌失败"` | `i18n.T(c, i18n.MsgTokenGenFailed)` |
| `"注册功能已关闭"` | `i18n.T(c, i18n.MsgRegisterDisabled)` |
| `"缺少 Authorization 头"` | `i18n.T(c, i18n.MsgMissingAuthHeader)` |
| `"无效的 Authorization 格式"` | `i18n.T(c, i18n.MsgInvalidAuthFormat)` |
| `"无效的 token: " + err.Error()` | `i18n.TWithDetail(c, i18n.MsgInvalidToken, err.Error())` |
| `"生成新 token 失败"` | `i18n.T(c, i18n.MsgTokenRefreshFailed)` |

### Step 6：修改 `log_handler.go`（5 处）

| 原文 | 替换为 |
|------|--------|
| `"日志不存在"` | `i18n.T(c, i18n.MsgLogNotFound)` |
| `"导出失败"` | `i18n.T(c, i18n.MsgExportFailed)` |
| `"无效的请求参数"` | `i18n.T(c, i18n.MsgInvalidParams)` |
| `"创建审计日志失败"` | `i18n.T(c, i18n.MsgAuditLogCreateFailed)` |
| `"审计日志已创建"` | `i18n.T(c, i18n.MsgAuditLogCreated)` |

### Step 7：修改 `module_registry_handler.go`（4 处）

| 原文 | 替换为 |
|------|--------|
| `"模块注册成功"` | `i18n.T(c, i18n.MsgModuleRegistered)` |
| `"心跳更新成功"` | `i18n.T(c, i18n.MsgModuleHeartbeat)` |
| `"模块不存在"` | `i18n.T(c, i18n.MsgModuleNotFound)` |
| `"模块注销成功"` | `i18n.T(c, i18n.MsgModuleDeleted)` |

### Step 8：修改 `cleanup_handler.go`（2 处）

`"无效的请求参数: " + err.Error()` → `i18n.TWithDetail(c, i18n.MsgInvalidParams, err.Error())`（2处）

### Step 9：重启验证

```bash
./scripts/dev/restart.sh -all   # 修改了 common，需全量重启
```

---

## 注意事项

- **service 层错误不翻译**：`err.Error()` 透传的业务错误（如"用户名已存在"）本次不处理
- **common/api/errors.go 的 error 变量不改**：用于 `errors.Is()` 匹配，不用于显示
- **TOML 使用带引号的点分 key**：`"err.invalid_id" = "..."` 是合法 TOML，unmarshal 到 `map[string]string` 后 key 为 `err.invalid_id`

---

## 验证方法

```bash
# 测试 token refresh 无 Authorization 头 - 英文
curl -X POST http://localhost:8180/api/v1/system/refresh \
  -H "Accept-Language: en"
# 期望：{"error": "Missing Authorization header"}

# 中文
curl -X POST http://localhost:8180/api/v1/system/refresh \
  -H "Accept-Language: zh-CN"
# 期望：{"error": "缺少 Authorization 头"}
```
