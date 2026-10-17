# ADDP Workbench 后续事项与验收

已实现的概念、契约、创作与运行规则唯一维护于[数据服务消费与数据应用规范](../spec/addp数据服务消费与数据应用规范.md)。本文仅保留未实施设计、历史最终验收与剩余边界，不替代当前 owner 实现。

## 一、延期能力

- Graph、Tile、Registered Service 适配；
- 多服务 JOIN；
- 自由 SQL 和计算字段语言；
- 多页面 Data Application；
- 后台定时刷新；
- 订阅、告警和邮件报表；
- 多页拼接、异步文件生成和无界结果导出；
- AI 自动生成分析或图表；
- 另一套缓存、物化和 OLAP 引擎。

延期能力不得以隐藏路由、占位表、禁用字段或前端硬编码形式预留。

## 二、验收与证据边界

### 2.1 标准入口

按[正式规范](../spec/addp数据服务消费与数据应用规范.md#十二验证入口与维护)和实际变更 owner 运行标准门禁；Online 继续使用已登记 `workbench-service-consumption`，本地证据不替代其隔离与清理报告。

### 2.2 已留存的验收结论

本节归并原文截至 2026-09-18 的记录，不表示本次文档整理重新执行了产品测试，也不推断当前环境状态。

| 范围 | 已有证据 | 边界 |
| --- | --- | --- |
| Workbench 模块 | 标准模块门禁曾完整通过 Backend、Frontend、构建、Swagger、平台登记与 PostgreSQL T2 | 后续改动仍按标准入口重验，不沿用历史测试数量作为当前结果 |
| 应用生命周期 | 真实浏览器完成草稿保存、不可变发布、顶层运行、参数/选择联动、分页和有限导出 | 已发布修订不能原地改写或物理删除 |
| Catalog / Asset / Portal | 真实浏览器完成目录收录、资产发布、申请、审批、履约、打开和撤销；普通用户验证同一应用 Grant 前拒绝、生效后运行、撤销后拒绝 | owner 自申请不能替代非所有者证据；该人工记录不等于可重复 Online T4 |
| Business MySQL | 真实固定 SQL JOIN、精确 ReadSet / 直接列 OutputLineage、SDK 与应用 Table / Chart、CSV 和契约显式修订 | 只证明本地异构链路，专用 T4 未完成 |
| 场景与展示 | 真实应用完成四类 renderer、专题图、字段/值名称与状态呈现、保存/重载/发布/自动首查；向导与预览完成无持久副作用验收 | 领域字段和样例值仅保存在具体应用 Snapshot，不进入生产默认配置 |
| 受控下钻 | 真实浏览器完成全期/非整月区间的汇总到去重明细点击、分页对账和零结果，并发布不可变 Revision | 同期 Go、Service PostgreSQL 与 Swagger 分项通过；完整 Service 模块门禁因工具下载 DNS 失败中止，未计为通过；不替代专用 T4 |
| 异步与恢复 | 可控 Promise 覆盖参数、Descriptor、路由、编辑、删除、向导和导出迟到响应；浏览器复核正常主路 | 依赖故障后的同页恢复由确定性测试覆盖，不宣称已逐项真实停服演练 |
| 外部消费 | Python SDK 离线与发布门禁、普通表/空间表 cursor 和指纹冻结、多服务真实只读消费 | 不替代 Power Query 产品宿主和外部 Client 全生命周期验收 |

### 2.3 Online T4 与非所有者授权边界

`workbench-service-consumption` 已实现并登记根 Makefile、Online workflow、Host Gate、Business MySQL 隔离夹具和 Chromium 浏览器验证。真实验收应保留 readiness、summary、online report、浏览器报告、Service 执行审计、应用日志与 `cleanup=passed` / `residual_resources=0` 证据。历史调度未取得通过结果，后续本地运行与确定性 runner 检查不能补记为 T4 通过；具备符合既有部署准入的专用环境后执行同一入口，不放宽隔离边界或接管个人开发环境。

suite 只创建未发布应用并在退出时按本轮 ID 清理，浏览器验证正式创作、Table / Chart、无空间时禁用 Map、参数筛选、CSV 与契约变化阻断。值名称映射还须验证保存回显、服务返回的维度名称、映射显示、CSV 保留原值及选择联动传递原值；缺少这些浏览器断言的报告必须拒绝。不可变发布由 Workbench 确定性浏览器门禁与 PostgreSQL owner 门禁覆盖，指标名称关联由 Model PostgreSQL / MySQL 金样覆盖。

非所有者授权必须证明同一已发布应用、同一 Tenant User 的“无 Grant 返回 403 → Asset 履约后返回当前 Revision → 撤销后再次 403”。Runtime 只读取 Workbench owner 规则，申请、审批、履约与撤销归 Asset，目录解析归 Catalog。正式 HTTP 路由与 PostgreSQL 门禁已覆盖该持久化状态序列，Frontend 覆盖受限页与登录回跳；确定性测试不计为真实跨账号浏览器 T4。

完整非所有者闭环若纳入 T4，须由统一 Online Fixture 在专用 Tenant 预置长期非业务的 owner、消费 User、已发布 Data Application、CatalogEntry 和 application Asset。每轮仅新建可撤销的申请/授权，先恢复无 Grant 基线，结束撤销并确认再次 403。不得每轮发布新的不可删除应用，不得手删 Revision、CatalogEntry 或迁移记录伪造清理成功，不复用本地长期业务示例。

### 2.4 长期示例保留边界

下列应用在原验收中已确认长期保留，不按临时测试对象删除或下线。这里只保留身份，历史 Revision 号、参数值和行数不作为当前运行事实；本次未读取或修改这些资源。

| 应用 | Data Application ID |
| --- | --- |
| `workbench-p4-demo` | `1714dcf7-f34e-4996-a8dc-3b88998ebe55` |
| `workbench-parameter-linkage-demo` | `d6c30859-15c8-4b88-964b-f2dd315fb923` |
| Outdoor 双服务分析应用 | `18c7223c-b5c0-4c25-ba28-648e85f44537` |
| Business MySQL 电商订单分析 | `c847d823-3314-42a2-b2a7-d5139fc68283` |
| `farmland-spatial-consumption` | `c4c0aa6e-70b1-49e8-8ade-8db92f5c6e33` |

## 三、后续推进边界

已确认的创作与展示范围已落地，后续按以下证据缺口推进：

1. 优先补齐 `workbench-service-consumption` 专用 Online T4 证据；本地异构功能、人工跨账号闭环和确定性 runner 测试均不能替代该门禁。
2. 获得 Windows / Power BI Desktop 宿主后，按下节设计实现唯一 Power Query Connector，先验证 callback state、Refresh Token 轮换和指纹冻结刷新，再编写正式接入指南。
3. Service owner Resource Grant / Policy 仍按原清单保留为外部前置项；接入前先核对统一授权规范，不在 Workbench 或 Service 中另建私有 ACL。
4. 真实数据量超过有界 GeoJSON 上限后，先定义 Tile / OGC Features 消费契约；之前不继续堆叠 renderer 或增加无界查询。
5. 只有确认成功打开次数、独立访问用户和 Revision 分布有独立产品价值后，才设计 Workbench owner 运行准入事实；跨模块综合统计、运行埋点、多页面、mobile、轮播、通用动作和后台任务继续暂缓。

后续实现若与公开契约冲突，先修订本专题及正式规范，不增加兼容路由、兼容字段、API Key 私有授权、数据库直连或 Workbench / Python 查询代理。


## 四、Power Query Connector 待实施设计

首个真实 BI 验收载体确定为 Power Query 自定义 Connector 与 Power BI Desktop Import。唯一代码位置为 `service/connectors/power-query/`，由 Service owner 维护产品适配器、测试、构建和安装说明，不进入 Service Backend 进程，也不依赖 Workbench。

Connector 尚未完成，需取得可操作的 Windows / Power BI Desktop 宿主。优先验证 callback state、Refresh Token 轮换和带指纹 Navigation Key 的刷新行为；没有宿主证据前不修改 ADDP OAuth 或 Service API，不宣称正式接入指南可用。

### 4.1 用户入口与身份

Connector 唯一入口拟定为：

```text
ADDPService.Contents(base_url, client_id)
```

- `base_url` 是当前 ADDP Gateway 的公开根地址；生产连接必须使用 HTTPS；
- `client_id` 是当前 Tenant 管理员在 System 中为该 Power Query 部署独立注册的公共 OAuth Client ID，不是 Secret，可以作为 Data Source Path 的组成部分；
- Connector 固定请求 `scope=addp.api`，不接受 Client Secret、API Key、用户名密码或内置 Client ID；
- Connector 使用 Power Query 宿主文档规定的标准 HTTPS callback URI，并必须原样登记到该 Tenant OAuth Client；不得自行猜测或降级到 `localhost`；
- 凭据作用域固定为 `base_url + client_id`，避免不同 ADDP 环境或不同 Tenant Client 误共享 Token。

OAuth 实现使用 Advanced Signature，以便 `Refresh` 和 `Logout` 同时取得 Data Source Path：

1. `StartLogin` 生成 PKCE S256 verifier/challenge，向 `/api/v1/system/oauth/authorization_requests` 发起表单 POST；
2. Connector 只把 System 返回的 `request_id` 放入 Console `/oauth/authorize?request_id=...`，并在内存 Context 保存 verifier、`request_id`、一次性 `request_secret`、`client_id` 和 callback URI；
3. `FinishLogin` 必须校验 callback 中的单一 `state` 等于 `request_id`，再用单一 `code`、原 callback URI 和 verifier 兑换 Token；
4. `Refresh` 必须接受并保存每次响应返回的新 Refresh Token，不能继续复用旧 Token；
5. `Logout` 调用 `/api/v1/system/oauth/revoke`；用户取消时若 Power Query 宿主提供可调用的取消边界，则使用 `request_secret` 取消 pending request，否则依赖 System 固定五分钟过期，不能增加第二套状态；
6. 外部 Client 被停用、用户 Permission 被撤销或 Refresh Token Family 被撤销后，旧连接必须失败，恢复 Client 不能使旧凭据重新生效。

Power Query 会向 `StartLogin` 传入宿主 `state`，而 ADDP 安全规范固定使用随机 `request_id` 作为回调 `state`。官方函数签名允许 Connector 在 `FinishLogin` 中取得 Context、callback URI 和 state，但是否有宿主级的额外等值限制仍必须以真实 Power BI Desktop 运行证明；在该证据出现前不修改 ADDP OAuth 规范，也不增加兼容 state 字段。

### 4.2 服务发现、Schema 与刷新

身份建立后，Connector 固定执行以下链路：

```text
GET Service Consumer Catalog
  -> 为每个服务读取 Consumer Descriptor
  -> 生成 Power Query Navigation Table
  -> 选择服务后调用 Descriptor query operation
  -> 按 cursor 获取下一页
  -> 按 output_contract 强制表结构和字段类型
```

具体规则：

1. Catalog 使用 `page/page_size` 完整枚举，不读取 Service 管理 API；Navigation Key 必须包含 `service_type + service_id + contract_fingerprint`，显示名称只使用 Descriptor title；
2. Power Query 保存的行选择会冻结上述 Navigation Key。刷新时若服务下线、权限消失或指纹变化，旧 Key 无法命中并显式失败，不能按标题、同名服务或新指纹自动改绑；
3. 每次读取数据仍重新取得 Descriptor，严格校验 `schema_version`、ServiceReference、指纹、operation method/path 和输入输出类型；只调用 Descriptor 声明的相对 path；
4. 第一版只使用 `format=json` 和 `X-ADDP-Query-Intent: query`。每页 `limit` 使用 Descriptor `page.max_limit`，下一页只使用响应 `page.next_cursor`，直到 `has_more=false`；不能解析 cursor、改用 offset 或从 URL 猜测下一页；
5. Connector 使用 Descriptor `default_selection` 建立默认列，并按 `output_contract.fields` 强制固定列顺序、nullable 和类型。`string/uuid/bool/int/bigint/float/double/date/time/timestamp` 映射为相应 Power Query 标量；缺少 precision / scale 证明的 `decimal`，以及 `bytes/mixed/json/array/geometry` 第一版保守映射为文本，不做有损数值或空间推断；
6. 当前 Descriptor 已能表达命名参数，但 Power BI Navigator 无法从运行时 Descriptor 动态生成稳定的 M 函数签名。第一版导航表只直接加载“不含无默认值必填 named parameter”的服务；同时保留显式 `parameters record` 的查询函数供后续稳定交互设计使用，不从字段名推断参数；
7. 第一版是 Import，不实现 DirectQuery 或 Query Folding。Power BI 的筛选、图表和模型在导入结果上工作；刷新重新执行 Service 查询。若未来需要把 Power Query 筛选下推为 ADDP structured filter，必须先独立定义可证明的 folding 子集，不能翻译任意 M 或 SQL。

### 4.3 验收门禁

只有以下证据全部完成后，才能勾选“真实 BI Connector 端到端验收”并编写面向用户的正式接入指南：

1. 使用 GitHub 托管 Windows Runner 构建唯一 `.mez` 制品，并运行 Power Query SDK query tests；根 `Makefile`、workflow 路径登记、依赖固定和制品校验必须与 Connector 同次实现；
2. 在 Power BI Desktop 中通过 System 创建的独立 Tenant OAuth Client 完成登录、Tenant Context、授权同意和 Navigator 打开；
3. 以至少一个普通表服务和一个空间表服务验证 Catalog、Descriptor、动态字段 Schema、多页 cursor、空结果和 nullable；空间 geometry 第一版作为文本列进入 BI，不宣称原生面渲染；
4. 验证 Access Token 过期后的 Refresh Token 轮换、并发或旧 Refresh Token 重用拒绝、Logout 撤销和 Client 停用；
5. 撤销 `service.data_read.execute` 后刷新返回权限错误；恢复 Permission 后由当前用户重新刷新，不缓存旧授权判断；
6. 修改测试服务公开契约后，既有 Power Query 查询因冻结指纹失败；用户重新在 Navigator 选择当前服务后才建立新查询；
7. 验证第二个不同来源的 Query Service，避免 Connector 偶然依赖 Outdoor、PostgreSQL、固定字段名或空间输出；
8. `.mez`、日志、测试快照和文档中不得包含 Access Token、Refresh Token、Authorization Code、PKCE verifier、request secret、筛选值或返回数据。


## 五、维护方式

完成后将稳定约定回写正式规范并从待办移除，只原位更新最终验收与缺口；不追加逐轮实施、重启或 CI 流水。
