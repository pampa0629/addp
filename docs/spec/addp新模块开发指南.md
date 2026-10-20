# ADDP 新模块开发指南

## ⚠️ 关键原则（必读）

新模块开发必须遵循以下原则，确保与现有模块的一致性：

1. **配置加载统一性**
   - ✅ Go 模块使用 `common/config.LoadEnv()` 补齐根环境配置，保留启动入口显式注入的值
   - ✅ 复用 `commonConfig.BaseConfig` 与 `LoadDeploymentConfig()`，普通运行配置归 owner 持久化管理
   - ❌ 禁止硬编码 `.env` 文件路径（如 `godotenv.Load("../../.env")`）
   - ❌ 禁止重复定义通用配置字段

2. **代码复用原则**
   - ✅ 通用功能必须提取到 `common/` 或 `common-frontend/`
   - ✅ 参考现有模块的实现模式（推荐：manager、meta）
   - ❌ 禁止在新模块中重复实现已有的通用功能

3. **Schema 隔离原则**
   - ✅ 每个模块使用独立的 PostgreSQL schema
   - ✅ owner Backend 使用唯一的版本化启动迁移创建 schema；Worker 只读校验版本
   - ✅ 公共 `common` schema 由 System Backend 初始化，业务 Backend 和 Worker 只校验
   - ✅ DSN 连接字符串必须包含 `search_path` 参数

4. **脚本集成原则**
   - ✅ 同步接入启动、重启、停止与实际端口解析，复用标准生命周期及内容指纹构建
   - ✅ 测试、构建和 CI 的自动发现或显式登记必须覆盖新模块
   - ✅ 支持独立启动模式（`-your-module`）
   - ✅ 支持全量启动模式（默认行为）

5. **国际化原则**
   - ✅ 新模块必须按 [ADDP 国际化开发规范](addp国际化开发规范.md) 创建前后端翻译文件
   - ✅ 前端用户可见文本必须使用 Vue I18n，不得硬编码中文或英文
   - ✅ 后端用户可见错误消息必须通过 `common/middleware/i18n` 翻译
   - ✅ Swagger 注解必须使用 `中文 | English` 双语格式

6. **Console 页面描述原则**
   - ✅ 侧边栏固定菜单页由 Console 自动记录最近访问；若侧边栏短标签依赖模块上下文，必须在 `SIDEBAR_MENUS` 中另设可独立识别的 `recentLabel`
   - ✅ 稳定动态页面在业务对象加载后使用 `useConsolePageDescriptor()` 声明页面标题和对象名称
   - ✅ 页面标题和对象名称必须国际化，并在语言切换后自动更新
   - ✅ 页面描述的 `title` 必须是脱离当前模块和页面上下文后仍可独立识别的业务对象类型，如“传输任务”“质量检查执行”；“任务详情”“执行详情”“服务信息”等局部页面标题不得直接复用
   - ✅ 页面描述的 `subject` 只承载具体对象名称、标题或业务编码；不得依赖对象名称中的偶然动词来补足 `title` 语义
   - ❌ Console 不解析模块动态路由，不跨模块查询业务对象
   - ❌ 创建、编辑、登录、OAuth 回调等临时页面不得进入最近访问

7. **启动依赖边界**
   - ✅ Backend 和附属 Worker 必须支持零 Engine Instance 启动并进入各自正常空闲状态
   - ✅ 模块可以把自身必需 Infra 作为启动或 readiness 条件；Infra 不属于 Engine Instance 启动解耦范围
   - ✅ 业务进程可在 System 不可达时保持 Alive 并后台恢复注册，但必须在 System 注册成功后才能 Ready
   - ✅ 其他业务模块不可达只失败依赖该能力的具体请求或 execution，不得成为本模块 Ready 条件
   - ✅ 引擎缺失、离线或能力不匹配只失败实际使用该引擎的请求或 execution
   - ✅ Engine Runtime 在自身服务就绪后异步自注册，注册失败不得阻塞 Runtime readiness
   - ❌ 模块启动、健康检查或构造函数不得查询、连接、等待或隐式拉起可选 Engine Instance / Engine Runtime

**违反以上原则的代码将无法通过 Code Review。**

---

## 后端接入步骤

实现或扩展服务时:

1. **遵循 System 模块模式**:

   ```
   <module>/backend/
   ├── cmd/server/main.go       # 入口点
   ├── internal/
   │   ├── api/                 # HTTP 处理器
   │   ├── service/             # 业务逻辑
   │   ├── repository/          # 数据访问
   │   ├── models/              # 数据结构
   │   ├── middleware/          # 认证、日志
   │   └── config/              # 配置
   └── go.mod                   # 模块依赖（跨模块通用能力归 common/）
   ```
2. **数据库约定**:

   - 使用 PostgreSQL schema 隔离 (所有模块使用 PostgreSQL 和专用 schemas)
   - 使用 GORM 作为 ORM；连接数据库与执行迁移分离，模块 schema 只由 owner Backend 的启动迁移负责
   - 在 `scripts/infra/init-postgresql.sql` 登记模块 schema 与注释；表、约束和索引由 owner 通过 `common/schema` 的事务锁与版本记录迁移，变更同步递增 `SchemaVersion`
   - Worker 只校验模块及公共 schema 版本，不执行 DDL；Backend Ready 后再启动所属 Worker。版本与启动顺序见[生命周期规范](addp开发服务生命周期与构建身份规范.md#模块迁移与-worker-启动顺序)
   - 使用 `updated_at` 触发器进行时间戳跟踪

3. **配置管理**（必须遵循）:

   先按[配置规范](addp配置介绍.md)区分事实来源，不将所有参数都放进环境变量或 System 配置 API：

   | 配置内容 | 唯一来源与加载要求 |
   | --- | --- |
   | 端口、数据库连接、必需 Infra 地址和启动前参数 | 根 `.env`、进程环境或容器部署注入；本机实际端口以标准入口解析结果为准 |
   | 密码、Service Client Secret、加密密钥 | 受控 Secret 或部署环境注入；不进入普通配置表、共享配置 API、日志或版本库 |
   | 平台或 Tenant 的普通运行配置 | owner 自己的持久化配置与范围规则，不以根环境变量或 System 下发值作 fallback |
   | Engine、任务定义、execution 参数和用户偏好 | 各自的强类型资源或执行快照，不改存为通用配置键值 |

   Go 模块的部署加载顺序：

   1. 在进程启动入口调用 `commonConfig.LoadEnv()`，只用根 `.env` 补齐缺失的环境变量；显式注入的实际端口和地址优先，禁止硬编码相对 `.env` 路径或增加私有覆盖文件。
   2. 模块配置嵌入 `commonConfig.BaseConfig`，通过 `commonConfig.LoadDeploymentConfig(&cfg.BaseConfig)` 加载共享部署字段；只增加模块真正需要的字段，不重复定义数据库字段或再建 `SystemURL` 镜像字段。
   3. 显式加载并校验模块监听配置、`BaseConfig.SystemServiceURL` 和所属 Service Client Secret 等必要参数。`LoadDeploymentConfig()` 不会替模块填充全部字段，不能把调用过加载器视为配置已完整。
   4. 数据库 DSN 指定 owner `search_path`，复用共享 Infra 配置能力；连接地址、SSL 模式和凭据按部署环境确定，不复制固定本机端口和数据库密码模板。

   共享实现见[部署配置加载器](../../common/config/loader.go)及[Infra 配置能力](../../common/config/service_config.go)。配置归属、默认值与生效方式统一遵循[配置归属与生效](configuration/配置归属与生效.md)，环境文件和 Secret 边界见[部署环境与密钥](configuration/部署环境与密钥.md)。

4. **认证**:

	- 复用 `common/middleware/auth` 调用 System AuthContext API
	- 从 AuthContext 读取 Principal、会话模式、唯一当前 Tenant、Scope 和必要授权事实并传递给服务层
	- 使用稳定 Permission 执行功能授权，禁止新增 `user_type` helper 或角色名称硬编码分支
	- 不读取 `JWT_SECRET`，不在模块内自行解析用户 Token
5. **国际化集成**:

   - 注册 `common/middleware/i18n.I18nMiddleware()`
   - 创建 `<module>/backend/i18n/i18n.go`
   - 创建 `<module>/backend/i18n/locales/zh-cn.toml` 和 `en.toml`
   - Handler 中的用户可见错误消息使用 `commoni18n.T(c, modulei18n.MsgXxx)`
   - Swagger 注解使用 `中文 | English` 格式

   详细要求参见 [ADDP 国际化开发规范](addp国际化开发规范.md)。

6. **Docker 集成**:

   - 在服务根目录创建 Dockerfile
   - 在根 `docker-compose.yml` 登记服务镜像、网络与必要依赖；产品镜像统一由根 `make build-images` 构建
   - 使用 `/health/live` 作为存活探针，使用 `/health/ready` 作为就绪探针
   - 连接到 `addp-network` 进行服务间通信
   - `depends_on` 只登记模块自身必需 Infra 和 System Ready；不得把其他业务模块或可选 Engine Runtime 设为 Backend/Worker 就绪条件

7. **开发脚本集成**（新模块必做）:

   按现有函数与模块分支定位接入点，不按历史行号复制脚本片段：

   | 接入位置 | 必须覆盖的行为 |
   | --- | --- |
   | `scripts/dev/start.sh` | 参数、模块选择与全量选择、真实依赖、Backend/Worker 构建和就绪顺序、前端启动清单 |
   | `scripts/dev/restart.sh`、`stop.sh` | 参数及所选进程的停止范围；核验工作区归属，局部操作不扩散停止依赖，重启复用标准 stop/start 路径 |
   | `scripts/dev/ports.sh` | 新服务端口登记、所属进程与模块映射、实际端口注入；不按固定端口接管未知进程 |
   | Swagger、构建与测试入口 | 新模块由现有自动发现或显式登记覆盖；同步 owner 测试、根 Make 入口、CI 路径选择与产品镜像登记 |

   模块分支只描述本体与真实依赖，公共依赖复用现有选择逻辑，不假定所有模块都需要 Meta。Engine Runtime 由自身显式参数或全量组合选择，业务模块不得隐式拉起可选 Engine Runtime；Manager 自有 Raster Mosaic Runtime 按既有 owner 入口管理。具体依赖组合见[模块选择指南](../../scripts/docs/模块选择启动指南.md)。

   Go 构建复用 `scripts/dev/build-identity.sh` 的内容指纹和原子发布路径。启动前核验受管进程与端口，构建完成后再次检查，再登记 PID 和检查 Ready；不得另写直接运行 `.dev-bins/`、重定向固定日志文件的旁路。Backend 服务运行日志遵循[运行日志规范](addp模块服务运行日志规范.md)。

   验证按[测试与验收规范](addp测试与验收规范.md)和[CI 与交付规范](testing/CI与交付规范.md)选择门禁：构建登记用 `make test-build-registration`，生命周期接入用 `make test-dev-lifecycle`，owner 行为用 `make test-module MODULE=<模块>`；自动发现已覆盖的事实不重复维护手写清单。

   单模块、全量启动和局部重启的运行验证使用[标准生命周期命令](../../scripts/docs/启动脚本使用指南.md)。这些操作用于运行联调，不代替测试；需要重启时由用户在自己的终端执行。

8. **前端集成**:

   按[前端开发工作流](#前端开发工作流)建立模块入口、共享认证、国际化与主题，再登记构建、Console 代理和部署路由。目录与依赖参考现有模块，不复制模块私有业务或整套认证实现。

## 开发工作流

### 添加新的 API 端点

先按[模块职责](../concepts/modules/平台总览与模块职责.md)和 owner 的 `CLAUDE.md` 确认接口归属，再按[API 规范](addp-API设计规范.md)确定请求、响应、资源路径与认证授权契约。不能将某个模块的 Engine 或 Resource 示例直接复制为所有业务的模型。

| 层级 | 职责与接入位置 |
| --- | --- |
| DTO / Model | 在模块的 `internal/models/` 等既有位置定义请求校验与响应结构，保持业务对象命名一致 |
| Handler | 在 `internal/api/` 解析请求、取得共享认证上下文并校验功能权限；处理 Service 返回值与错误，写回响应并维护 Swagger 注解 |
| Service | 在 `internal/service/` 执行业务规则、事务边界及跨 Repository 编排；返回领域结果和错误，不依赖 `gin.Context` |
| Repository | 在 `internal/repository/` 封装数据库或存储访问，供 Service 调用 |
| Router | 在模块既有路由入口登记唯一公开路径，使用 `/api/v1/<module>` 前缀并接入认证与授权中间件 |

按[响应与错误规范](api/响应与错误.md)复用共享响应和错误映射，处理校验失败、业务失败与成功结果，不能忽略错误后直接返回成功。用户可见错误由 Handler 或统一响应层按请求语言翻译，见[国际化规范](addp国际化开发规范.md#service-层错误)。

新增或修改公开接口时，同步调用方、双语注解和生成产物，并执行[Swagger 强制同步与覆盖检查](addp-Swagger集成指南.md#强制同步要求)。Operation 授权声明与 Permission 事实按该指南维护；路由覆盖检查不能代替授权契约校验。具体分层要求见[API 文档与实施规范](api/文档与实施.md#后端实现分层与共享能力)。

### 数据库迁移

结构变更沿用 owner Backend 的唯一启动迁移路径；同步模型、迁移、`SchemaVersion` 与 owner migration 测试，并由根 Make / CI 标准入口验证。禁止另建根目录集中 SQL、手工集中迁移入口或第二套启动迁移路线。

已有版本化 migration 只新增向前版本，不修改已发布文件。`common/schema` 的事务锁与版本记录不替代 owner 的既有 SQL 迁移目录；Backend 独占迁移、Worker 只读校验及结构升级时的停止顺序，遵循[模块迁移规范](addp开发服务生命周期与构建身份规范.md#模块迁移与-worker-启动顺序)。

修改 Meta 技术元数据时，同步核对[Meta 模型](../../meta/backend/internal/models/)、扫描读写与消费方契约。attributes 的标准分区、`schema_version` 和历史数据处理遵循[元数据 attributes 规范](addp元数据attributes规范.md)；旧 attributes 不保留兼容读写，应按规范清理后重新扫描，不在此另设数据迁移模板。

### 添加前端页面

页面归属以[模块职责](../concepts/modules/平台总览与模块职责.md)和对应模块说明为准；跨模块通用能力先检索 `common-frontend`，模块只保留领域组合与协议适配。

1. 在 owner 的 `<module>/frontend/src/views/` 创建页面，业务 API 放在 `src/api/`，复用[共享客户端与认证](#开发中的前端-后端连接)。
2. 在 `src/router/index.js` 登记页面，受保护页面作为 Layout 子路由；路由基础路径使用正式模块入口，共享守卫等待会话和 AuthContext 初始化后再判断访问权限，见[登录认证规范](addp登录认证的统一要求.md#五console-与-iframe)。
3. 在 `src/components/Layout.vue` 组合模块布局和导航。独立顶层界面显示 header、sidebar 和 content；Console iframe 通过 `isInIframe` 识别，只显示内容区域与 `<router-view>`，避免重复外壳。详情、表单等子页应保持所属父菜单激活。
4. 页面标题、菜单和用户可见文本按[国际化规范](addp国际化开发规范.md)使用翻译词条；稳定动态页面在对象加载后声明 Console 页面描述，遵守本文关键原则中的最近访问边界。
5. Layout 和页面使用 `var(--addp-bg-primary)`、`var(--addp-bg-secondary)`、`var(--addp-border-color)` 等主题变量；主题初始化只按[新模块前端步骤](#创建新模块前端)接入。布局、样式覆盖和 iframe 容器要求见[前端风格规范](../../common-frontend/docs/addp前端风格设计规范.md)。

登录、会话恢复和退出复用共享 AuthStore / AuthSession，不能在 Layout 再建 Token 存储或刷新逻辑。页面验证覆盖所属 API、权限拒绝、独立顶层与 iframe 展示、父菜单激活、语言和主题切换；按 owner 标准门禁区分组件测试与浏览器验收。

## 前端开发工作流

### 快速开始: Console + 所有模块

从项目根目录使用标准开发入口启动前后端及公共依赖：

```bash
bash scripts/dev/start.sh
```

通过本次启动输出的 Console 地址访问平台。Node 版本以根 `.node-version` 为准，依赖按模块锁文件使用 `npm ci`；启动入口会检查并准备缺失依赖。仅修改支持热更新的前端代码时直接查看更新，确需重启时由用户在终端执行，见[启动脚本使用指南](../../scripts/docs/启动脚本使用指南.md)。

### 运行单个前端 (独立模式)

例如只联调 Manager 及其依赖：

```bash
bash scripts/dev/start.sh -manager
```

启动范围见[模块选择启动指南](../../scripts/docs/模块选择启动指南.md)。模块独立顶层界面和 Console iframe 都使用 Console 同 origin 的 `/module-ui/{frontend}/...`；Portal 使用 `/portal/...`，Workbench 数据应用使用 `/data-apps/:application_id`。独立界面保留模块自己的布局。

模块开发端口是内部服务端口，直接打开时必须在页面脚本执行前跳转到 Console 同 origin 的正式入口，并保留 path、query 和 fragment。跨站点独立部署在该站点登录；具体边界见[登录认证规范](addp登录认证的统一要求.md#五console-与-iframe)，不另建认证入口。

### 开发中的前端-后端连接

浏览器业务与认证请求统一经当前 origin 的 `/api/v1` 进入 Gateway，开发环境由 Vite 代理到实际 Gateway 地址。开发服务地址取本次启动解析结果，不在业务客户端写死后端端口。

模块的 `api/client.js` 使用共享客户端；其默认 `baseURL` 为 `/api/v1`，调用方传 `/<module>/resources`，最终路径为 `/api/v1/<module>/resources`：

```javascript
import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

export default createAPIClient(() => useAuthStore(), {
  moduleName: 'YourModule'
})
```

认证 API 使用共享 `createAuthAPI()`，指向同 origin 的 `/api/v1/system`。模块通过 `createAuthStore()`、`createAuthGuard()` 复用共享 Browser AuthSession；Access Token 仅在内存保存，iframe 由父 Console 协调认证与刷新，顶层页面通过 Cookie 恢复会话。完整要求以[登录认证规范](addp登录认证的统一要求.md)为准；资源路径按[API 路由规范](api/资源路由与命名.md)定义。

### 创建新模块前端

1. 在新的 `<module>/frontend/` 下按现有模块组织 Vue 3、Pinia 和 Element Plus 工程，调整包名、标题、业务视图和菜单。只参考需要的结构，不整目录复制已有模块的业务、依赖或构建产物。
2. 使用根 `.node-version` 对应的 Node，提交模块 `package.json` 和一致的 `package-lock.json`。通过 `overrides` 及 Vite 别名、去重规则保持 Vue 单一实例；common-frontend 复用消费模块依赖，不独立安装自己的 `node_modules`。
3. Vite 复用共享 `withModuleFrontend()`，并在其模块登记及 Console 的代理、菜单配置中接入新入口。Router 使用 `createWebHistory(import.meta.env.BASE_URL)`；模块公开前缀、内部开发端口和 Console 地址按[认证规范](addp登录认证的统一要求.md#五console-与-iframe)及[端口规范](addp端口分配.md)配置，不各自维护另一套跳转逻辑。
4. 创建本模块 AuthStore 与路由守卫以调用共享认证能力，业务请求使用上节客户端。按 Layout 嵌套路由组织页面，支持独立顶层界面和 Console iframe。
5. 按[国际化规范](addp国际化开发规范.md)创建 `src/i18n/zh-cn.json` 与 `en.json`，使用共享 `createAddpI18n()`；iframe 监听 Console 语言变化，Element Plus locale 跟随当前语言，请求统一携带 `Accept-Language`。
6. Layout 背景与边框使用主题 CSS 变量；在 `main.js` 引入 Element Plus 基础 CSS 后，引入深色变量和共享主题，并在挂载前初始化：

   ```javascript
   import 'element-plus/theme-chalk/dark/css-vars.css'
   import '@common-ui/styles/theme.css'
   import { useTheme } from '@common-ui'

   // app.use() 完成后、app.mount() 之前
   const { init: initTheme } = useTheme({ listenToConsole: true, storageKey: 'theme-mode' })
   initTheme()
   app.mount('#app')
   ```

7. 共享能力从 `@common-ui` 复用；地图能力按实际需要引入 `@common-ui-map` 及其依赖，不按模块名称预设依赖集合。别名、导出与组件契约见[共享前端说明](../../common-frontend/README.md)，颜色、布局及唯一所有权要求见[前端风格规范](../../common-frontend/docs/addp前端风格设计规范.md)。
8. 按[前端镜像构建规范](../../scripts/docs/前端镜像构建规范.md)维护 Dockerfile、静态输入和模块 Nginx 配置，登记唯一构建入口、根 Compose 服务及统一 Nginx 路由。构建上下文为仓库根目录，Compose 消费标准入口产出的镜像；根 Compose 只发布 Nginx 宿主端口，不为模块前端增加独立端口或 `full` profile。

镜像必须支持正式入口下的 history 深层路由刷新，以及所属 assets、预览插件和 WASM 等资源路径。Manager 的特殊资源与本地依赖布局只在确有相同输入时参考，不作为所有模块的通用复制模板。

## 常见错误和排查

### 错误 1: ".env file not found"

核对标准入口是否注入部署环境，以及 `commonConfig.LoadEnv()` 定位的根目录。加载器只补齐缺失值；容器已显式注入配置时，缺少本地 `.env` 不等同于缺少必要配置。禁止改用 `../../.env` 等硬编码路径，处理边界见[部署环境规范](configuration/部署环境与密钥.md#根目录环境配置唯一路径)。

### 错误 2: "schema does not exist"

核对连接的 database、owner `search_path`、Backend 迁移结果和版本记录。初始化脚本中的 schema 登记不能代替 owner 迁移；Worker 遇到缺失或不匹配应明确失败，不能自行创建表或修复版本。按[模块迁移顺序](addp开发服务生命周期与构建身份规范.md#模块迁移与-worker-启动顺序)处理。

### 错误 3: 重启脚本不识别新模块

核对 start、restart、stop 参数及端口归属登记，按[开发脚本集成要求](#后端接入步骤)检查是否完整；不能只补帮助文本或绕过标准脚本直接运行二进制。

### 错误 4: 配置字段重复定义

数据库字段、`SystemServiceURL`、共享日志配置等复用 `commonConfig.BaseConfig`；删除同一事实的重复字段和读取路径。模块特有配置按[配置管理要求](#后端接入步骤)声明来源，不通过字段复制维持另一套值。

### 错误 5: DSN 缺少 search_path 参数

检查 DSN 是否包含正确的 owner `search_path`，以及模块迁移是否在正确 schema 执行。发现表落入 `public` 时，先查清现有数据和迁移状态，再由 owner 处理；不要以追加第二套启动建表路径掩盖问题。

### 错误 6: 模块 Alive 但未 Ready，Gateway 无法路由

**现象**: `/health/live` 返回 200，但 `/health/ready` 返回 503，Gateway 无法路由请求

**原因**: 模块尚未向 System 成功注册，或心跳失败后正处于 `recovering`

**解决方案**:
在 `cmd/server/main.go` 中使用统一模块生命周期能力，不得只调用注册函数却不把注册状态接入 Ready 门禁。启动顺序固定为：

1. 校验部署配置并初始化自身必需 Infra；
2. 创建可取消的进程信号 Context；
3. 绑定 HTTP Listener，注册 `/health/live` 和 `/health/ready`；
4. 用该 Context 启动统一模块注册生命周期，`health_check_url` 只指向 `/health/ready`；
5. 在健康路由之后、业务路由之前安装 Ready 门禁；
6. 进程退出前取消 Context，并等待限时注销完成。

注册声明的 `health_check_url` 必须指向 `/health/ready`；共享生命周期与注册恢复语义以[生命周期规范](addp开发服务生命周期与构建身份规范.md)和[健康与平台控制面规范](api/健康与平台控制面.md)为准。

不得使用 `context.Background()` 启动进程级注册，不得在端口绑定之前注册，也不得为了“注册成功”在入口同步阻塞等待 System。

### 错误 7: 前端 API 路径包含重复的 /api 前缀

**现象**：请求 URL 出现 `/api/v1/api/v1/...` 等重复前缀。

**原因与处理**：共享 `createAPIClient()` 默认 `baseURL` 为 `/api/v1`，调用方只传 `/<module>/resources`，最终路径为 `/api/v1/<module>/resources`。核对浏览器实际请求、客户端前缀与调用参数，再按[前后端连接说明](#开发中的前端-后端连接)修正重复部分，不通过代理重写保留错误路径。

### 错误 8: 前端模块不跟随 Console 主题切换

先按[主题接入步骤](#创建新模块前端)检查 `main.js` 是否引入 Element Plus 深色变量和共享主题 CSS，并在挂载前调用 `useTheme({ listenToConsole: true })`。

再检查 Layout、iframe 内容容器和页面的背景、边框是否使用 ADDP 主题变量，以及 scoped 样式和 Element Plus 覆盖是否符合[前端风格规范](../../common-frontend/docs/addp前端风格设计规范.md)。不要复制第二份主题初始化；切换 Console 主题后逐层确认实际生效样式。

## 检查清单

开发新模块时，使用此清单确保所有步骤完成：

**后端开发**:
- [ ] 复制并调整目录结构 (`backend/cmd/server/`, `backend/internal/`)
- [ ] 配置使用 `commonConfig.LoadEnv()` 和 `BaseConfig`
- [ ] DSN 包含 `search_path` 参数
- [ ] 在 `init-postgresql.sql` 中登记 owner schema
- [ ] owner Backend 通过 `common/schema` 执行唯一版本化迁移，结构变更递增 `SchemaVersion`，并有 migration 测试和 CI 入口
- [ ] Worker 只读校验 owner 与公共 schema 版本，Backend Ready 后启动，不执行 DDL
- [ ] 实现唯一存活和就绪端点 (`/health/live`、`/health/ready`)
- [ ] 使用统一模块生命周期接入注册、心跳、Ready 门禁和注销

**前端开发**:
- [ ] 建立前端目录与模块锁文件，Node 版本取根 `.node-version`，安装使用 `npm ci`
- [ ] 登记开发首选端口，并消费标准启动入口解析的实际端口
- [ ] 共享 Vite 模块登记、Console 代理和部署路由均能命中正式入口，Router 使用相同基础路径
- [ ] 实现 Layout 组件（支持双模式）
- [ ] **Layout.vue 背景/边框使用 CSS 变量（`var(--addp-bg-primary/secondary)`，非硬编码颜色）**
- [ ] **main.js 导入主题 CSS 并调用 `useTheme({ listenToConsole: true })`**
- [ ] 配置 common-frontend 别名、依赖去重与 `overrides`，保持 Vue 单一实例
- [ ] 新建组件前已检索并复用 `common-frontend` 和现有模块的规范实现，没有重复建设相同语义职责与交互契约的前端能力
- [ ] 共享 AuthStore、路由守卫和 API Client 已接入，认证请求经同 origin Gateway
- [ ] API Client 默认前缀 `/api/v1` 与调用方路径 `/<module>/resources` 拼接正确，没有重复前缀
- [ ] 中英文、Element Plus locale、Console 语言同步和 `Accept-Language` 已接入
- [ ] Dockerfile 与模块 Nginx 配置符合[前端镜像规范](../../scripts/docs/前端镜像构建规范.md)

**脚本集成**:
- [ ] 修改 `scripts/dev/start.sh`（参数、启动标志、全量启动、模块 case、编译、启动、前端配置；单模块公共依赖不要重复写）
- [ ] `scripts/dev/restart.sh` 的帮助、参数与停止范围能识别新模块，仍复用标准启动与构建路径
- [ ] `scripts/dev/stop.sh` 与 `ports.sh` 的模块、进程和端口登记完整
- [ ] Go 构建复用内容指纹与原子发布路径，生命周期接入通过 `make test-dev-lifecycle`
- [ ] 根 `Makefile` 能自动发现或显式提供模块测试入口，`.github/workflows/` 的路径选择能命中该模块
- [ ] 正式服务、Worker、前端、Python 包、PostgreSQL 门禁和 Dockerfile 的自动发现或显式登记完整；构建登记使用 `make test-build-registration`，平台其余登记按[CI 与交付规范](testing/CI与交付规范.md)验证
- [ ] 运行 `make test-module MODULE=<模块>`；提交前再运行 `make test-changed`
- [ ] 验证独立启动 (`-your-module`)
- [ ] 验证全量启动

**文档和配置**:
- [ ] 在受版本控制的 `.env.example` 与端口规范中登记首选配置，并按配置规范同步本地环境
- [ ] 创建模块的 `CLAUDE.md` 文档
- [ ] 更新根目录 `CLAUDE.md` 的模块列表

**测试验证**:
- [ ] System 不可达时模块 `/health/live` 通过、`/health/ready` 返回 503，业务路由返回 `module_not_ready`
- [ ] System 恢复后同一 `instance_id` 自动注册，`/health/ready` 恢复 200
- [ ] 数据库表创建在正确的 schema
- [ ] 模块注册到 Gateway 成功
- [ ] 前端可以访问后端 API
- [ ] Console 可以嵌入模块前端
- [ ] **切换 Console 主题，模块前端背景/边框随之变化**
- [ ] 独立顶层入口与 Console iframe 的登录、刷新和退出均遵循共享认证；直接访问开发端口会跳转到正式入口
- [ ] 真实镜像下刷新受保护页面的深层 URL，并验证所属静态资源可达

按[测试与验收规范](addp测试与验收规范.md)记录各层结果：登记静态检查、模块测试、真实镜像构建与浏览器运行验收分别陈述；未运行、失败或跳过项不能勾选为通过。联调需要重启开发服务时，由用户在自己的终端执行标准生命周期命令。
