# @addp/common-frontend

ADDP 平台前端共享组件库，提供跨模块复用的 Vue 3 组件、工具函数和类型定义。

前端开发先阅读[前端风格设计规范](docs/addp前端风格设计规范.md)，再按视觉与布局、组件交互、主题接入或工程约束进入对应专题。

## 使用

入口、依赖与职责见[共享前端架构](docs/ARCHITECTURE.md)；下表按使用场景导航到具体契约与示例。

## 组件列表

| 使用场景 | 文档入口 |
| --- | --- |
| locator、资源选择、树展示、树缓存与增量加载 | [资源定位与树组件](docs/components/资源定位与树组件.md) |
| 页面刷新、任务监控、Console 导航、最近访问、离页保护与草稿移交 | [页面状态与导航](docs/components/页面状态与导航.md) |
| 文件及空间预览、表格图表结果、图与血缘、Agent UI、DAG 查看器 | [预览与结果展示](docs/components/预览与结果展示.md) |
| 表单、参数、业务域、布局、调度、认证入口、工具与类型 | [基础控件与工具](docs/components/基础控件与工具.md) |

## 安装

```bash
# 在模块的 frontend 目录中
npm install file:../../common-frontend
```

## 测试

共享组件、结果契约和唯一所有权测试由 `common-frontend` 自己维护，统一入口为：

```bash
npm --prefix common-frontend test
```

根 `make test-platform` 会调用该入口。业务模块只验证自身的协议适配和组合行为，不再代跑共享组件测试。

### 浏览器测试隔离

确定性浏览器门禁通过 `scripts/test/frontend-browser-gate.mjs` 和共享 `withBrowserTestIsolation` 统一隔离当轮端口、代理、地址与输出目录。模块在 `package.json` 声明 `addpBrowserTest.fixtures`，不得复制启动、端口分配或缓存清理逻辑；浏览器断言使用 `browserTestOrigin`。继续使用原有 `make test-<owner>-frontend` / `npm run test:e2e` 入口。

Vite 配置直接导入 `basic/src/utils/viteTestIsolation.mjs`，以 `defineConfig(withFrontendTestIsolation(模块名, 配置对象))` 包装。`ADDP_E2E=1` 时统一关闭 HMR，使用模块和进程独立的临时依赖缓存，并在 Vite 关闭、进程正常退出及收到 `SIGTERM` / `SIGINT` 时清理；普通开发返回原配置。Playwright 每个 `webServer` 显式传入该标记、回环地址、独立端口与 `--strictPort`，且不得复用现有服务；配置带正数超时的 `gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 }`，避免 Playwright 默认强制结束进程而留下缓存。隔离逻辑及清理测试归共享库，配置接入检查归平台 T0。

## 开发

### 添加新组件

先按[工程约束](docs/frontend-style/工程约束.md#组件开发规范)检索已有实现，再在所属子目录的现有组织中维护组件。基础 UI 放 `basic/src/components/`，地图组件放 `map/src/components/`；公共导出维护在对应 `src/index.js`，文件预览使用 `basic/src/previews.js` 独立入口。组件职责与使用说明同步到对应文档。

### 添加新工具函数

工具函数放在所属子目录的既有工具位置，补充 JSDoc 与最小充分测试，再从对应入口导出；不在已经不存在的根 `src/utils/` 下新建一套路径。

### 添加新类型

基础共享类型维护在 `basic/src/types/` 并按现有入口导出；业务私有类型由业务模块维护，不将 owner 的 DTO 放入共享层。

## 依赖

依赖与 peer dependency 以[包声明](package.json)为准，消费模块版本统一遵循[技术栈规约](../docs/spec/addp技术栈规约.md)并维护自己的锁文件；各共享入口的职责与按需依赖见[架构说明](docs/ARCHITECTURE.md)。

## 许可

MIT
