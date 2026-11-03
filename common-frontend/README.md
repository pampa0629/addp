# @addp/common-frontend

ADDP 平台前端共享组件库，提供跨模块复用的 Vue 3 组件、工具函数和类型定义。

前端开发先阅读[前端风格设计规范](docs/addp前端风格设计规范.md)，再按视觉与布局、组件交互、主题接入或工程约束进入对应专题。

阅读导航：[入口与用法](docs/ARCHITECTURE.md#使用方法) · [组件契约](#组件列表) · [资源选择](#资源树选择器接入导航) · [页面导航](#页面状态与导航接入) · [DAG](#dag-接入与验证) · [安装接入](#安装) · [共享验证](#测试) · [开发约束](#开发)。

## 使用

入口、依赖与职责见[共享前端架构](docs/ARCHITECTURE.md)；下表按使用场景导航到具体契约与示例。

## 组件列表

| 使用场景 | 文档入口 |
| --- | --- |
| locator、资源选择、树展示、树缓存与增量加载 | [资源定位与树组件](docs/components/资源定位与树组件.md) · [选择器接入导航](#资源树选择器接入导航) |
| 页面刷新、任务监控、Console 导航、最近访问、离页保护与草稿移交 | [页面状态与导航](docs/components/页面状态与导航.md) · [接入导航](#页面状态与导航接入) |
| 文件及空间预览、表格图表结果、图与血缘、Agent UI | [预览与结果展示](docs/components/预览与结果展示.md) |
| DAG 编辑、只读展示、API 与消费方验证 | [接入与验证导航](#dag-接入与验证) |
| 表单、参数、业务域、布局、调度、认证入口、工具与类型 | [基础控件与工具](docs/components/基础控件与工具.md) |

职责、权限复核和失败语义统一维护在以下专题；共享门禁与消费方验收的分工见[测试](#测试)，不以导航或示例核对替代实际接入验收。

### 资源树选择器接入导航

| 阅读目的 | 直达正文 |
| --- | --- |
| 共享组件与 Owner 分工、请求会话 | [共享所有权与访问会话](docs/components/资源定位与树组件.md#共享所有权与访问会话) |
| 选择模式、资源身份、回显与结果字段 | [身份模式与回显](docs/components/资源定位与树组件.md#身份模式与回显) |
| 多选顺序、完整快照、异步核验与失败回退 | [多选快照与异步核验](docs/components/资源定位与树组件.md#多选快照与异步核验) |
| 文字筛选、资源搜索与下拉交互 | [紧凑输入与选择](docs/components/资源定位与树组件.md#紧凑输入与选择) |
| 原生目录、适配职责与搜索失败边界 | [实时目录适配](docs/components/资源定位与树组件.md#实时目录适配) |
| System 候选、节点与数据项选择、边界绑定 | [System 授权候选与边界绑定](docs/components/资源定位与树组件.md#system-授权候选与边界绑定) |
| 只读事实核对、失败状态与签发前复核 | [原生事实核对与签发限制](docs/components/资源定位与树组件.md#原生事实核对与签发限制) |
| 已有表选择、新表目标与实际验收范围 | [选择与创建示例](docs/components/资源定位与树组件.md#选择与创建示例) |

### 页面状态与导航接入

| 阅读目的 | 直达正文 |
| --- | --- |
| 未提交筛选、提交权限、刷新协作与失败边界 | [自动筛选与已提交范围](docs/components/页面状态与导航.md#自动筛选与已提交范围) |
| 在途请求取消、迟到响应与 Owner 状态清理 | [请求取消与迟到响应隔离](docs/components/页面状态与导航.md#请求取消与迟到响应隔离) |
| 模块内与跨模块导航、历史选择、权限落点与导航失败 | [模块公开导航与历史](docs/components/页面状态与导航.md#模块公开导航与历史) |
| 数据元冻结引用、确定修订与历史切换 | [数据元确定修订跳转](docs/components/页面状态与导航.md#数据元确定修订跳转) |
| 受控主机选择、部署清单校验及 System／Monitor 分工 | [主机选择与部署清单](docs/components/页面状态与导航.md#主机选择与部署清单) |
| 一次性草稿移交、Owner 核验、清理与实际验收范围 | [Console 草稿移交](docs/components/页面状态与导航.md#console-草稿移交) |

### DAG 接入与验证

按职责进入[编辑器接入](dag/README.md#基础-dag-编辑器)、[公共 API](dag/README.md#api)、[操作规则](dag/README.md#交互与布局约束)、[只读查看器](docs/components/预览与结果展示.md#只读-dag-查看器)或[验证分工](dag/README.md#验证分工)。

## 安装

模块接入统一按[创建新模块前端](../docs/spec/addp新模块开发指南.md#创建新模块前端)准备消费模块的依赖声明、锁文件和工程配置；共享入口、别名与 Vue 单一实例约束见[接入与验证](docs/ARCHITECTURE.md#接入与验证)。

## 测试

共享组件、结果契约和唯一所有权测试由 `common-frontend` 自己维护，统一入口为：

```bash
make test-common-frontend
```

根 `make test-platform` 调用同一入口。业务模块只验证自身的协议适配和组合行为，不代跑共享组件测试；本入口不代替消费模块的浏览器回归或真实后端验收。门禁选择见[测试与验收规范](../docs/spec/addp测试与验收规范.md)。

### 浏览器测试隔离

确定性浏览器门禁通过 `scripts/test/frontend-browser-gate.mjs` 和共享 `withBrowserTestIsolation` 统一隔离当轮端口、代理、地址与输出目录。模块在 `package.json` 声明 `addpBrowserTest.fixtures`，不得复制启动、端口分配或缓存清理逻辑；浏览器断言使用 `browserTestOrigin`。运行统一从已登记的 `make test-<owner>-frontend` 进入，模块浏览器脚本与 fixture 启停由标准门禁编排。

Vite 配置直接导入 `basic/src/utils/viteTestIsolation.mjs`，以 `defineConfig(withFrontendTestIsolation(模块名, 配置对象))` 包装。`ADDP_E2E=1` 时统一关闭 HMR，使用模块和进程独立的临时依赖缓存，并在 Vite 关闭、进程正常退出及收到 `SIGTERM` / `SIGINT` 时清理；普通开发返回原配置。Playwright 每个 `webServer` 显式传入该标记、回环地址、独立端口与 `--strictPort`，且不得复用现有服务；配置带正数超时的 `gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 }`，避免 Playwright 默认强制结束进程而留下缓存。隔离逻辑及清理测试归共享库，配置接入检查归平台 T0。

真实 HMR 集成探针保持 `ADDP_E2E=1`，在 `ADDP_BROWSER_RUN` 中提供当轮临时 `runtimeDir` 和 `hmr: true`；共享 helper 保留原 HMR 配置，继续隔离缓存、源码和清理生命周期。不能为测试 HMR 而关闭整个测试隔离，否则链接复用的 `node_modules/.vite` 会被测试进程改写。

## 开发

### 添加新组件

先按[工程约束](docs/frontend-style/工程约束.md#组件开发规范)检索已有实现，再在所属子目录的现有组织中维护组件。基础 UI 放 `basic/src/components/`，地图组件放 `map/src/components/`；公共导出维护在对应 `src/index.js`，文件预览使用 `basic/src/previews.js` 独立入口。组件职责与使用说明同步到对应文档。

System 与各业务模块显示 IAM 角色时，统一从 `@common-ui` 导入 `resolveRoleName`、`resolveRoleDescription`。内置 `roles.*` 译名唯一维护在 `basic/src/i18n/`，自定义角色名称保持原输入；业务模块不得复制角色译名或保留转发实现。

### 添加新工具函数

工具函数放在所属子目录的既有工具位置，补充 JSDoc 与最小充分测试，再从对应入口导出；不在已经不存在的根 `src/utils/` 下新建一套路径。

### 添加新类型

基础共享类型维护在 `basic/src/types/` 并按现有入口导出；业务私有类型由业务模块维护，不将 owner 的 DTO 放入共享层。

## 依赖

依赖与 peer dependency 以实际使用入口的包声明为准，例如[根包](package.json)、[Chart](chart/package.json)和 [Map](map/package.json)。消费模块版本统一遵循[技术栈规约](../docs/spec/addp技术栈规约.md#前端依赖版本规范)并维护自己的锁文件；各共享入口的职责与按需依赖见[架构说明](docs/ARCHITECTURE.md#使用方法)。

## 许可

MIT
