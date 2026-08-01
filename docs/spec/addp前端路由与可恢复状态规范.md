# ADDP 前端路由与可恢复状态规范

## 一、目标与适用范围

本文定义 ADDP Console 与各业务模块前端的公开 URL、可恢复页面状态和浏览器历史规则。所有通过 Console iframe 集成、同时支持 standalone 运行的前端模块均适用。

目标是保证用户能够通过刷新、复制链接、打开新标签页以及浏览器前进/后退恢复同一业务上下文，同时避免把临时交互状态、敏感信息或大体量业务数据写入 URL。

## 二、公开路由事实源

1. Console iframe 模式下，浏览器地址栏中的 Console URL 是唯一公开路由事实源。
2. 子模块 Vue Router 只负责当前 iframe 内的渲染状态，不得形成与 Console 地址栏分叉的第二套公开历史。
3. standalone 模式下，模块自身 URL 是公开路由事实源，并复用相同的模块内 path 和 query 契约。
4. Console 路由固定为 `/{module}{moduleFullPath}`。模块不得硬编码 Console 端口、直接修改 `window.parent.history` 或自行实现 `postMessage` 导航协议。
5. 跨模块导航、模块内公开导航和地址栏同步统一使用 `common-frontend` 提供的 Console navigation 能力。

## 三、必须进入 URL 的状态

满足以下任一条件的状态必须进入 URL：

1. 决定当前业务对象身份，例如任务 ID、执行 ID、资源 Locator、服务 ID、模型 ID。
2. 决定当前页面职责，例如创建、编辑、详情、执行结果或审批视图。
3. 用户刷新或复制链接后合理期望恢复的稳定子视图，例如 item 的预览 / 剖析 / 属性 Tab。
4. TaskProvider `create_url` / `edit_url`、Monitor 回跳或跨模块入口依赖的参数。

推荐表达方式：

| 状态 | URL 位置 | 示例 |
| --- | --- | --- |
| 独立资源或详情页身份 | path parameter 优先 | `/transfer/tasks/42/edit` |
| 一个编辑器页面承载多种任务类型 | canonical query | `/develop/workflow?action=edit&id=544` |
| 稳定视图、Tab、筛选条件 | query | `?tab=profile` |
| 跨模块资源身份 | 规范定义的稳定身份 | `?locator={ResourceLocator}` |

同一概念只能有一个参数名。不得同时接受 `id`、`taskId`、`task_id` 等多个身份参数；具体名称由 owner 模块规范确定。

## 四、不得进入 URL 的状态

以下状态保留在组件、Store 或服务端草稿中：

1. 未保存表单内容、工作流 DAG JSON、SQL 正文和 Notebook 内容。
2. 弹窗开关、悬停、焦点、滚动位置、分隔栏宽度、画布缩放和平移。
3. 短时加载、进度、错误提示、确认框和乐观更新状态。
4. Access Token、Refresh Token、浏览器资源访问票据、引擎凭据、连接信息和任何密钥。

页面刷新后必须恢复的未保存内容，应使用 owner 模块明确设计的草稿能力，不能依赖无限扩张 query 或浏览器存储旁路。

## 五、Canonical URL

1. 每个可恢复页面只允许一个 canonical URL；旧 path、旧 query 名称和兼容读取分支必须删除。
2. 默认值应从 URL 省略，但省略后必须有唯一确定的含义。
3. URL 解析完成后发现非规范大小写、缺失默认动作或服务端返回更准确身份时，应使用 `replace` 规范化为 canonical URL。
4. ID 无效、对象不存在或用户无权访问时，页面必须显示对应错误状态；不得静默打开另一个对象，也不得保留看似有效的错误 URL。
5. 创建动作完成并获得稳定 ID 后，必须从创建 URL `replace` 为编辑或详情 URL。

Develop TaskProvider 的 canonical 前端路由为：

```text
/develop/sql?action=create
/develop/sql?action=edit&id={dev_task_id}
/develop/workflow?action=create
/develop/workflow?action=edit&id={dev_task_id}
/develop/notebook?action=create
/develop/notebook?action=edit&id={dev_task_id}
```

`/develop/tasks` 只表示任务定义列表，不得在编辑器已加载具体任务后继续作为地址栏 URL。Develop 任务身份参数固定为 `id`，不得保留 `taskId` 双轨。

当前已落地的稳定状态参数如下；新增同类状态必须优先复用 owner 模块已有命名，不得增加别名：

| 模块 | 页面或状态 | Canonical 表达 |
| --- | --- | --- |
| Manager | 数据资源与预览子视图 | `locator`、`tab` |
| Manager | 快显与空间任务工作区 | `tab`、`task_id`、`create=1` 及页面定义的创建来源参数；默认任务 Tab 省略 |
| Develop | SQL、工作流、Notebook 创建或编辑 | `action`、`id` |
| Develop | 执行列表筛选与分页 | `dev_type`、`status`、`trigger_type`、`source_task_id`、`start_date`、`end_date`、`page`、`page_size` |
| Orchestrator | 编排创建与编辑 | path `/orchestrations/new`、`/orchestrations/:id/edit` |
| Graph | 本体/审核稳定 Tab、知识服务当前图谱 | `tab`、`graph_id` |
| Service | 服务目录类型 Tab | `tab`，默认 `all` 省略 |
| Modeling | 实体详情 Tab、星型模型事实表 | `tab`、`table_id` |
| Quality | 执行详情 | path parameter `execution_id` |
| Quality | 检查任务创建与编辑 | `create=1`、`task_id`；默认列表省略 |
| Asset | 资产目录、申请与反馈 Tab | `catalog_id`、`tab` |
| Meta | 扫描引擎与扫描任务入口 | `engine_id`、`task_id`；二者并存时任务所属引擎为事实源 |
| System | IAM、引擎详情与审计筛选 | `tab`、path `/engines/:id`、`event_name`、`result`、`risk_level`、`module_name`、`entity_type`、`entity_id`、`page` |
| Portal | 搜索、目录分页与资产详情 | `keyword`、`type_id`、`page`、path `/portal/catalogs/:id`、`/portal/assets/:id` |
| Agent | 当前会话 | path `/sessions/:session_id`，Console 公开 URL 为 `/agent/sessions/:session_id` |

模块内 Router path 不得重复携带 Console 模块前缀。例如 Modeling 模块内使用 `/entities/:id`，Console 公开 URL 才是 `/modeling/entities/:id`。

Portal 是 Console 当前 origin 下的独立用户门户，不使用 iframe 模块导航桥；其公开路由固定保留 `/portal` 前缀。详情返回只允许使用已验证的 Portal 内部历史或 owner 数据推导的固定回退目标，不接受任意 `return_url`。

## 六、浏览器历史语义

| 用户动作 | 历史模式 | 说明 |
| --- | --- | --- |
| 列表进入详情或编辑器 | `push` | 后退返回来源列表 |
| 从一个业务对象切换到另一个对象 | `push` | 前进/后退恢复对象身份 |
| 跨模块进入任务、执行或资源页面 | `push` | 保留来源上下文 |
| 创建成功后写入新对象 ID | `replace` | 后退不返回已经失效的创建状态 |
| 同一对象切换高频 Tab、视图或规范化参数 | `replace` | 避免历史膨胀 |
| 清除已删除对象的 ID | `replace` | 当前历史项不再指向不存在对象 |

Console iframe 模式下，一次用户导航只能产生一条公开历史记录。禁止子模块先 `router.push()`、再让 Console `push()` 的双 push 路线。共享导航能力应在 iframe 内用本地 `replace` 完成无刷新渲染，再由 Console 按指定的 `push` 或 `replace` 更新地址栏。

## 七、Console 与模块同步

1. 模块内已经完成的同步导航只更新 Console URL，不得重载活动 iframe。
2. 浏览器前进/后退、直接打开深链接以及跨模块导航由 Console 根据公开 URL 加载目标 iframe 路由。
3. Console 只接受活动模块 iframe 发出的同步请求；同步目标必须仍属于当前模块。
4. 模块导航失败时必须暴露失败，不得回退到硬编码端口、旧 query 或第二条导航路径。
5. 页面不得只在 `onMounted` 读取身份参数；同组件复用时还必须响应 canonical path/query 的变化，保证 standalone 前进/后退一致。

## 八、实现与验收要求

共享实现必须位于 `common-frontend`。业务模块只负责：

1. 定义本模块 canonical route builder。
2. 在用户导航时明确选择 `push` 或 `replace`。
3. 根据当前 canonical route 加载 owner 业务对象。
4. 删除旧参数、旧路由和直接操作父窗口的实现。

每个迁移页面至少验证：

1. 直接打开带 ID 的 Console URL 能恢复正确对象。
2. 刷新后对象和稳定子视图不丢失。
3. 列表进入编辑器后地址栏包含 canonical ID。
4. 浏览器后退返回来源，前进重新恢复目标对象。
5. 同步导航不会重复增加历史，也不会无故重载 iframe。
6. standalone 模式使用相同模块内 URL 契约。
7. URL 中不包含 Token、票据、凭据或未保存业务内容。
