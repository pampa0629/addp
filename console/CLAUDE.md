# Console 模块说明

## 模块定位

Console 是 ADDP 的统一前端入口，负责登录、全局导航、主题/语言切换、模块健康和 Swagger 文档入口，并通过 iframe 集成各业务模块前端。

导航项的 `permissions` 默认表达任一权限；需要组合授权的入口显式声明 `permissionMode: 'all'`。菜单和搜索均通过 `matchesNavigationAccess` 解释，不在页面重复过滤。Quality 概览需要方案、工单及执行读取权限全部具备。

## 技术栈与端口

- 前端：Vue 3 + Vue Router + Pinia + Element Plus。
- 开发首选端口：`5170`，启动脚本解析后的实际端口由 `CONSOLE_FE_PORT` 传入。
- 开发代理：`/api` 统一代理到启动时解析出的 Gateway 端口。

## 重要目录

```text
console/frontend/
├── src/
│   ├── views/             # Login、Portal、ApiDocs
│   ├── components/portal/ # PortalHeader、PortalSidebar、PortalIframe、PortalHome
│   ├── config/            # portalConfig、searchIndex
│   ├── store/             # auth、lang、theme
│   └── api/               # auth、client、copilot、meta
├── vite.config.js
└── README.md
```

## 开发规则

- 新增前端模块入口时，优先更新 `console/frontend/src/config/portalConfig.js`，并同步健康检查或 Swagger 代理配置。
- “数据准备”分组及首页卡片按 Transfer、Meta、Security、Manager 排列；“数据治理”分组包含 Standard、Model、Quality、Ontology。分组只表达产品导航，Security 继续独立拥有数据保护控制面，纳管仍由用户显式发起。
- Ontology 的具体入口为“领域本体 → 领域本体建模”（`/ontology/ontologies`），仅当前 Tenant 且具有 `ontology.revision.read` 时显示；权限过滤后没有可见子项的模块不渲染空父菜单，不自动扩张角色权限。
- Console 只做入口聚合，不承载业务模块的核心业务逻辑。
- 前端样式遵守 `common-frontend/docs/addp前端风格设计规范.md`，不要硬编码 ADDP 主题色。
- 各模块仍应支持独立运行，Console iframe 集成不能破坏 standalone 模式。
- Console 外层 URL 是 iframe 集成模式下的公开路由事实源。子模块通过共享 Console navigation bridge 同步自身路由；同模块已完成的导航只同步地址栏，不得重载 iframe，浏览器前进/后退或跨页面导航再由 Console 驱动 iframe 到目标路由。公开状态分类、canonical URL 和单历史约束统一遵守 `docs/spec/addp前端路由与可恢复状态规范.md`。
- Portal 是独立顶层产品界面，但正式入口固定为当前 Console origin 的 `/portal/`；开发环境由 Console Vite 代理到 Portal 前端，不能直接打开 `5185` 形成第二个顶层认证 origin。

## 开发与验证

```bash
bash scripts/dev/start.sh -console
cd console/frontend && npm run build
```

访问：`http://localhost:5170`

## 相关文档

- `console/frontend/README.md`
- `common-frontend/CLAUDE.md`
- `common-frontend/docs/addp前端风格设计规范.md`
- `docs/guide/addp部署和开发步骤.md`

Console 通过共享 `useConsoleUnsavedChangesGuard` 拦截活动 iframe 有未保存修改时的菜单、跨模块及历史导航。模块内部已完成确认的同步导航不重复确认；桥接返回取消结果。`make test-console-frontend` 包含确定性测试、离页与认证浏览器回归及构建，Platform CI 同步安装 Chromium。

查询服务离页回归在现有 Console 宿主夹具中加载真实 `QueryServiceForm.vue` 和 Service API 客户端，HTTP 响应由 Playwright 拦截，不依赖或写入开发数据库。该入口需要 Console 与 Service 两个前端的锁定依赖；测试自动启动 4170/4180 两个 Vite 服务，通过仅 E2E 启用的代理加载 iframe，覆盖 SQL/参数保留、内部及宿主导航、历史、刷新和保存失败/成功，以及已有服务编辑的版本冲突、确认重载、同组件身份切换和旧响应隔离。Service 前端变更经共享改动矩阵触发 Console 门禁，CI 同步安装 Service 依赖。

数据服务导航浏览器回归复用正式 `PortalSidebar`、`PortalIframe`、菜单配置及 Service 的 `App`、`Layout` 和路由表。`service-navigation.spec.js` 在同一门禁中覆盖五个入口的唯一导航、刷新、前进/后退、目录两个管理按钮的 iframe 保留与单历史项、注册卡片详情返回，以及独立访问菜单一致性；夹具仅用 hash history 隔离 URL，认证由现有认证浏览器门禁单独验证。

`registered-service-unsaved.spec.js` 复用同一正式导航夹具，覆盖注册表单的内部返回、Console 菜单、历史与刷新保护，普通字段、关键词及三类认证输入的保留，以及创建/更新失败后继续编辑、提交期间禁用输入、成功后的保护清除，以及确认离开后迟到的创建/更新成功和失败响应不干扰新草稿；所有写请求均由 Playwright 模拟，不写业务数据库。
