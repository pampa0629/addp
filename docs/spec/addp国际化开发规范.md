# ADDP 国际化开发规范

本文档定义 ADDP 前端、后端和 Swagger 文档的国际化开发约束。新增或修改用户可见文本、后端错误消息、API 文档注解时必须遵守本规范。

## 基本原则

1. 所有用户可见文本必须国际化，不新增硬编码中文或英文。
2. 默认语言为 `zh-cn`，英文为 `en`。
3. 语言偏好由前端持久化在 `localStorage` 的 `addp-lang` 中，并通过 HTTP `Accept-Language` 传递给后端。
4. 翻译文件遵循就近原则：公共词条放共享模块，业务词条放业务模块。
5. 运行时错误消息使用后端 i18n；Swagger 注解双语化只用于 API 文档展示，不能替代运行时错误翻译。

## 语言代码

ADDP 统一使用以下语言代码：

| 语言 | 代码 | 说明 |
| --- | --- | --- |
| 简体中文 | `zh-cn` | 默认语言 |
| 英文 | `en` | 当前内置英文 |

前端遇到浏览器语言 `zh-*` 时归一为 `zh-cn`，遇到 `en-*` 时归一为 `en`。后端 `Accept-Language` 解析规则与前端保持一致。

## 前端规范

### 文件组织

共享 UI 词条：

```text
common-frontend/basic/src/i18n/
├── zh-cn.json
└── en.json
```

模块业务词条：

```text
<module>/frontend/src/i18n/
├── zh-cn.json
└── en.json
```

如果模块依赖 `common-frontend/map` 或 `common-frontend/graph`，应合并这些共享包导出的消息，不能复制共享词条。

### 初始化

模块前端必须使用 `common-frontend/basic/src/composables/useAddpI18n.js` 中的 `createAddpI18n()` 初始化 Vue I18n。

```javascript
import { createAddpI18n } from '@common-ui/composables/useAddpI18n'
import zhCnMessages from './i18n/zh-cn.json'
import enMessages from './i18n/en.json'

const { i18n, init } = createAddpI18n({
  moduleMessages: {
    'zh-cn': zhCnMessages,
    en: enMessages
  },
  listenToConsole: true
})

app.use(i18n)
init()
```

Console 自身使用 `listenToConsole: false`，并通过顶部 `LangSwitcher` 作为全局语言入口。

### 组件使用

Vue 组件中使用 Vue I18n 的 `t()` 或共享 `useAddpI18n()`，不得在模板、脚本、表单校验、空状态、按钮、消息提示中硬编码用户可见文本。

```javascript
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
```

```vue
<template>
  <el-button>{{ t('common.save') }}</el-button>
</template>
```

### Key 命名

前端 key 使用命名空间分层：

```text
common.confirm
common.cancel
lang.zhCn
system.user.title
manager.preview.loading
transfer.task.status.running
```

规则：

- 公共词条使用 `common.*` 或共享组件所属命名空间。
- 模块词条以模块名开头，例如 `system.*`、`manager.*`。
- 同一业务对象下按页面、功能、状态继续分层。
- 不以显示文案作为 key；key 表达语义，不表达具体语言。

### Element Plus

使用 Element Plus 的模块必须让 Element Plus locale 跟随当前语言切换，确保日期、分页、选择器等组件语言一致。

```javascript
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import en from 'element-plus/es/locale/lang/en'

const elementLocale = computed(() => locale.value === 'zh-cn' ? zhCn : en)
```

### HTTP 请求

模块前端发送 API 请求时必须携带当前语言：

```http
Accept-Language: zh-cn
Accept-Language: en
```

如使用共享 API 客户端或 axios 拦截器，应从 `addp-lang` 读取语言并统一注入请求头。

## 后端规范

### 文件组织

通用基础设施：

```text
common/middleware/i18n/
├── i18n.go
├── translator.go
└── locales/
    ├── zh-cn.toml
    └── en.toml
```

模块业务消息：

```text
<module>/backend/i18n/
├── i18n.go
└── locales/
    ├── zh-cn.toml
    └── en.toml
```

`common/middleware/i18n/locales` 只放跨模块通用消息。模块业务错误、操作结果和提示消息必须放在模块自己的 `i18n/locales` 中。

### 路由中间件

所有 Go HTTP 模块必须注册 i18n 中间件：

```go
import i18nmiddleware "github.com/addp/common/middleware/i18n"

router.Use(i18nmiddleware.I18nMiddleware())
```

中间件从 `Accept-Language` 解析语言并写入 `gin.Context`。Handler 中不应自行解析请求头。

### 模块 i18n 包

模块后端应提供独立 `i18n` 包，定义消息 key 常量并注册 TOML 文件。

```go
package i18n

import (
    "embed"

    commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

const (
    MsgInvalidToken = "system.auth.invalid_token"
)

func init() {
    commoni18n.RegisterBundle(localeFS, "locales")
}
```

### TOML 格式

后端翻译文件使用 go-i18n 支持的 TOML message file 格式：

```toml
[system.auth.invalid_token]
other = "无效的 token"
```

```toml
[system.auth.invalid_token]
other = "Invalid token"
```

common 通用消息当前也兼容带引号的扁平 key 格式；新增模块业务消息应优先使用 section 格式，便于扩展复数和模板参数。

### Handler 使用

Handler 返回用户可见错误或提示时，使用 `commoni18n.T()` 或 `commoni18n.TWithDetail()`。

```go
import (
    commoni18n "github.com/addp/common/middleware/i18n"
    sysi18n "github.com/addp/system/i18n"
)

c.JSON(http.StatusUnauthorized, gin.H{
    "error": commoni18n.T(c, sysi18n.MsgInvalidToken),
})
```

动态详情只用于可暴露给用户或开发者的安全信息：

```go
commoni18n.TWithDetail(c, commoni18n.MsgInvalidParams, err.Error())
```

禁止在 Handler 中新增 `"参数错误"`、`"Invalid parameter"` 等硬编码用户可见消息。

### Service 层错误

Service 层优先返回结构化错误、领域错误或原始错误，不直接依赖 `gin.Context`。是否翻译由 Handler 或统一错误响应层根据当前请求语言处理。

如某个领域错误需要跨多个 Handler 复用，应定义稳定错误类型或错误码，再在 Handler 层映射到 i18n key。

## Swagger 双语注解规范

Swagger 注解中面向人阅读的描述使用 `中文 | English` 格式，中文在前，英文在后。

```go
// @Summary 获取用户列表 | Get user list
// @Description 根据条件分页查询系统用户列表 | Query system user list with pagination
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Success 200 {object} response.Data "返回用户列表 | Return user list"
```

要求：

- `@Summary`、`@Description`、`@Param` 描述、`@Success`/`@Failure` 描述应尽量双语。
- `@Router`、类型名、字段名、枚举值不翻译。
- Swagger 双语注解修改后必须重新生成文档并运行覆盖校验。

```bash
bash scripts/swagger/gen-swagger.sh <module>
bash scripts/swagger/check-route-coverage.sh <module>
```

涉及多个模块时：

```bash
bash scripts/swagger/gen-swagger.sh all
bash scripts/swagger/check-route-coverage.sh all
```

## 新增模块检查清单

新增前端模块时：

- 创建 `src/i18n/zh-cn.json` 和 `src/i18n/en.json`。
- 使用 `createAddpI18n()` 初始化 Vue I18n。
- iframe 模块启用 Console 语言监听。
- API 请求统一携带 `Accept-Language`。
- Element Plus locale 跟随 ADDP 当前语言。

新增 Go 后端模块时：

- 注册 `I18nMiddleware()`。
- 创建模块 `backend/i18n` 包和 `locales` 文件。
- 用户可见错误消息使用模块 i18n key。
- Swagger 注解使用 `中文 | English` 格式。

## 新增语言

新增语言时需要同步：

1. 在共享前端和各模块前端 `i18n/` 中增加对应 JSON 文件。
2. 在 `common/middleware/i18n/locales/` 和各模块后端 `i18n/locales/` 中增加对应 TOML 文件。
3. 扩展 `SUPPORTED_LANGS`、`SUPPORTED_LANGUAGES` 和后端语言归一化逻辑。
4. 增加 Element Plus 对应 locale 映射。
5. 补充验证：前端运行时切换、刷新后保持、后端错误消息、Swagger 展示。

## 验证方式

前端验证：

- 切换语言后当前页面文本立即更新。
- 刷新页面后保持所选语言。
- Console 切换语言后 iframe 模块同步更新。
- Element Plus 日期、分页等组件跟随切换。

后端验证：

```bash
curl -X POST http://localhost:8180/api/v1/system/refresh \
  -H "Accept-Language: en"
```

应返回英文错误消息。

```bash
curl -X POST http://localhost:8180/api/v1/system/refresh \
  -H "Accept-Language: zh-cn"
```

应返回中文错误消息。

Swagger 验证：

```bash
bash scripts/swagger/check-route-coverage.sh all
```

