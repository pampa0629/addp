# Console 模块说明

## 模块定位

Console 是 ADDP 的统一前端入口，负责登录、全局导航、主题/语言切换、模块健康和 Swagger 文档入口，并通过 iframe 集成各业务模块前端。

导航项的 `permissions` 默认表达任一权限；需要组合授权的入口显式声明 `permissionMode: 'all'`。菜单和搜索均通过 `matchesNavigationAccess` 解释，不在页面重复过滤。Quality 概览需要方案、工单及执行读取权限全部具备。

## 技术栈与端口

- 前端：Vue 3 + Vue Router + Pinia + Element Plus。
- 开发首选端口：`5170`，启动脚本解析后的实际端口由 `CONSOLE_FE_PORT` 传入。
- 开发代理：`/api` 统一代理到启动时解析出的 Gateway 端口。
- 模块 iframe 与独立界面统一使用当前 origin 的 `/module-ui/{frontend}/`；开发代理由 `common-frontend` 的 `createModuleFrontendProxies()` 统一生成，模块配置使用 `withModuleFrontend()`，直接打开模块开发端口先跳转到同源入口。

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
- Transfer 有任务读取权限时，Console 侧栏只显示“传输任务”，创建操作由任务列表的主按钮进入；仅有创建权限且具备 `meta.catalog.read` 时，侧栏以“创建传输任务”作为模块入口。创建页对前者保持任务列表菜单选中，对后者保持创建菜单选中；直接地址仍按各页面 Permission 校验。
- Ontology 的具体入口为“领域本体 → 领域本体建模”（`/ontology/ontologies`），仅当前 Tenant 且具有 `ontology.revision.read` 时显示；权限过滤后没有可见子项的模块不渲染空父菜单，不自动扩张角色权限。
- Console 只做入口聚合，不承载业务模块的核心业务逻辑。
- 右上角账号菜单只提供一个“个人中心”入口 `/system/account`，由 System 页面分类展示基本信息、组织归属和账号安全。无需 IAM 管理 Permission；分类以 `tab=organization|security` 恢复，默认基本信息省略 query。Console 不复制组织成员事实或个人页面逻辑。
- AuthContext 刷新期间清空旧候选权限，隐藏并保留当前路由 iframe；加载失败提供重试。同一 Principal、Context 和 Tenant Membership 重新获得当前页权限后恢复原实例，权限撤销、身份或上下文改变、退出及跨页导航卸载旧实例。`make test-console-frontend` 覆盖未保存草稿、故障重试和身份隔离；真实 Nginx T4 必须确认 iframe 实例及内存状态未因刷新丢失。
- 模块管理查询恢复浏览器回归加载真实 System 前端，验证 Console 地址栏同步不重载 iframe、刷新和新标签恢复组合筛选与分页、单历史及前进/后退。Console 门禁和 CI 同时准备 System 锁定依赖；System 前端改动自动触发该宿主门禁。
- Security 认证刷新回归加载真实纳管和字段策略表单，以 `Outdoor.Persons`、`userInfo.phone` 为资源与字段样例，验证资源选择、算法参数和调整依据在权限重新确认后保留；HTTP 响应使用确定性夹具，不写开发数据库。Console 门禁和 CI 同时准备 Security 锁定依赖，Security 前端改动自动触发宿主门禁。
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

Elasticsearch 正式 T4 浏览器用例 `e2e/online/elasticsearch-consumer-flow.spec.js` 通过真实 Console 登录同一普通用户，覆盖 Meta 重扫及稳定索引身份、Manager 文档分页与空索引、Develop Monaco 编辑器的 ES DSL 执行。多行 JSON 使用浏览器剪贴板和粘贴键输入，核对实际预检请求的完整查询及允许结果，避免逐字输入触发括号自动补全；执行结果读取页面自身的轮询响应并核对同一 execution_id，不使用整页导航前捕获的旧令牌。对应编辑器输入与执行响应回归由 `make test-develop-frontend` 验证；真实部署用例只由 Hosted Online 专用部署执行，产出四张截图和同一身份的报告，不计入确定性前端夹具 T3。

Manager 的 `manager-internal-artifact-lineage` Hosted suite 使用同一普通消费身份分两阶段验证 DAE／3DS：生成前检查真实 Console 中的 `generate_model_3d_glb` 入口；API 生成完成后再检查 ready GLB 的缓存消费、内容与渲染，此时生成按钮必须消失且浏览器不得重复生成。两个阶段共用现有 Playwright 用例和报告校验，点云、PPTX、COG 及领域清理断言保持完整。`make test-manager-online-runner` 验证阶段顺序、报告完整性和失败清理；真实链路仍由原 Hosted suite 验收。
