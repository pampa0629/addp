# ADDP 测试与验收规范

本文定义 ADDP 测试分层、标准入口、环境边界、CI 编排和 Online / Release 验收的稳定规则。具体可用目标、suite 登记和运行参数以根目录 `Makefile`、`scripts/README.md`、`scripts/test/` 及模块 owner 文档为准。

## 一、基本原则

1. 每项测试必须归入 T0-T5，并明确 owner、依赖、数据边界和触发方式。
2. 平台只统一协议、入口、编排、安全检查和报告；业务夹具、断言、清理及残留核验归 owner 模块。
3. 根 `Makefile` 是测试与验收的唯一公共入口，复杂准备和安全检查放在 `scripts/test/`；不得保留模块私有旁路、兼容别名或在 workflow 中复制业务测试逻辑。
4. 测试结果只有通过、失败和按规范明确跳过三种语义。缺少依赖、凭据或未实际执行测试不得伪装成通过。
5. 开发服务生命周期与测试生命周期分离。`scripts/dev/restart.sh` 不隐式运行测试，日常测试入口也不接管开发者已经启动的服务。
6. 专用 T4 Runner 可以在宿主机准入通过后编排隔离测试部署，但不得复用或接管个人开发环境。

## 二、测试分层

| 层级 | 名称 | 典型内容 | 运行依赖 | 默认触发 |
| --- | --- | --- | --- | --- |
| T0 | 静态与契约检查 | 格式、依赖与登记一致性、授权清单、Swagger 路由覆盖、测试夹具约束 | 无运行中服务 | 每次改动、PR |
| T1 | 单元与组件测试 | Go package、Python 单元、Repository mock、Vue 组件与状态逻辑 | 无，或进程内临时资源 | 每次改动、PR |
| T2 | 模块集成测试 | Migration、Repository、事务、锁、真实 PostgreSQL / Redis / 对象存储 | disposable 基础设施 | 相关模块改动、PR |
| T3 | 前端 Smoke / E2E | 路由、权限提示、状态恢复、关键表单和浏览器交互 | 独立测试端口或受控夹具 | 相关前端改动、PR |
| T4 | 跨模块 Online 验收 | 真实 System 认证、Gateway 路由、多个 owner、Worker、补偿和最终一致性 | 隔离的完整 ADDP 测试部署 | 手工；首跑稳定后可夜间执行 |
| T5 | 发布认证 | 安装包生命周期、真实 OS 凭据后端、HA、故障切换、在线厂商证据 | 产品或 Runtime 专用认证环境 | 发布前、Tag 或手工执行 |

T0-T3 证明确定性代码与模块集成；T4 证明真实运行拓扑；T5 证明产品或 Runtime 的发布条件。测试文件所在目录、与某个发布 workflow 共用编排，都不能改变其层级。

## 三、标准入口

平台公共入口为：

```bash
# T0-T1：全仓无外部服务确定性门禁
make test

# 当前工作区受影响模块的 T0-T3 门禁
make test-changed
make test-changed BASE_REF=<ref>

# 指定 owner 模块的 T0-T3 门禁
make test-module MODULE=<module>

# 全部已登记 disposable 基础设施门禁
make test-integration

# 一个已登记的跨模块 Online suite
make test-online ONLINE_SUITE=<suite>

# 一个已登记的产品发布 suite
make test-release RELEASE_SUITE=<suite>
```

约束如下：

- `make test-changed` 与 CI 共用 owner 影响计算；共享代码改动按真实依赖扩散到已登记消费者。
- `make test-module` 自动发现模块的 Go、Python、前端与基础设施门禁，不维护第二份模块清单。
- `make test-integration` 严格串行调用 owner 的 T2 事实入口，不复制测试逻辑。
- `test-online` 与 `test-release` 必须显式选择已实现的 suite；未实现能力不得以占位 suite 登记。
- 不提供跨 T0-T5 的 `test-all`。T4 与 T5 的身份、基础设施和安全前置条件不同，完整认证由 CI 分别编排标准入口并分别报告。
- 开发者和 AI 不得自行拼接一组语言命令替代上述标准入口；模块内部命令仅用于 owner 开发与标准入口实现。

## 四、T0-T3 确定性与基础设施边界

### 4.1 T0-T1

T0-T1 必须：

- 不依赖已经启动的 ADDP 服务。
- 不连接开发业务数据库。
- 可重复执行，失败后不留下外部状态。
- 需要 Online 条件的测试默认不进入普通语言测试；对应专用门禁显式开启并拒绝意外 Skip。
- 模块生命周期公共契约必须在 T1 覆盖 `starting → registered`、`registered → recovering → registered`、确定性拒绝进入 `failed`、取消后 `stopped` 与限时注销。
- Backend 路由测试必须证明 `/health/live` 不触发外部调用，`/health/ready` 只在自身必需 Infra 和 System 注册都就绪时返回 200，业务路由在 Not Ready 时返回 `503 module_not_ready`。
- Worker/Scheduler 测试必须证明未注册或 `recovering` 时不领取新工作，恢复后无需重启进程即继续领取。

### 4.2 T2

T2 使用真实但可丢弃的基础设施，并满足：

- CI Job 使用独占 Service 和随 Job 销毁的数据库。
- 托管外部服务的 owner gate 必须在脚本头声明 `# ADDP_T2_SERVICES=<service,...>`；模块门禁、CI 注册和后续新增数据库类型都消费该声明，不按数据库名称维护发现分支。
- 本地共享 `addp-postgres` 只允许使用 `addp_test` 与 `addp_iam_test`，并且只能通过根 `Makefile` 或 `scripts/test/` 的标准入口操作。
- 禁止为单次验证直接创建或删除数据库；现有标准入口不能满足隔离时，先完善入口及自动清理。
- 门禁在任何破坏性动作前校验数据库身份，拒绝开发库、生产库或不满足 owner 安全约束的连接。
- 每个场景只清理自己拥有的 Schema 或带唯一运行标识的事实，并验证零残留。
- 门禁拒绝意外 Skip，避免“命令成功但测试未运行”。
- Manager 派生任务定义、语义唯一约束、资源绑定与资源回收的持久化契约由 `make test-manager-postgres` 在真实 PostgreSQL 中覆盖；该门禁只允许本地 `addp_test` 或 CI 随 Job 销毁的 disposable database。

### 4.3 T3

T3 的 PR 主路径使用独立端口、受控 API 夹具和非个人登录态。真实 System、Gateway、owner Backend、真实身份与数据源的浏览器链路归入 T4，不与确定性浏览器测试混跑。

浏览器测试重点证明布局、路由、权限反馈、状态恢复、关键交互和响应式行为，不重复后端已经覆盖的全部字段或业务规则。

## 五、T4 Online 验收协议

### 5.1 专用环境

T4 只在隔离的 ADDP 测试部署执行，并按运行条件选择唯一部署 profile：

- 常规 Online suite 使用带 `self-hosted`、`macOS`、`addp-online` 标签的专用 Runner 和 `addp-online` GitHub Environment，复用专用部署中的稳定 Tenant、User 和 Engine Instance。
- 只有明确声明 Linux/CPU 限制的 suite 可登记 GitHub Hosted profile。该 profile 每轮必须在干净 `ubuntu-24.04` x86_64 Runner 上从零启动 disposable Infra、Tenant、User、Engine Instance 和业务引擎，退出时全部销毁；不使用 GitHub Environment 或仓库 Secret。
- self-hosted Runner 使用独立账号和独立 checkout；Hosted Runner 使用 Actions 当次临时 checkout。两者都不复用个人开发工作区或开发服务进程。
- 服务只绑定 Runner 可访问的回环地址；通用预检拒绝外部服务地址。
- 仓库根不得保存 T4 `.env`。self-hosted profile 的 Tenant、数据库连接和凭据由仓库外绝对路径环境文件注入；Hosted profile 只能使用当次 disposable 部署产生、位于 `runner.temp` 的 owner-only 凭据文件。
- `ADDP_ONLINE_HOST` 必须精确为 `1`；Hosted profile 还必须同时校验 `GITHUB_ACTIONS=true`、`RUNNER_OS=Linux` 和 `ADDP_ONLINE_HOSTED=1`。生命周期门禁必须在任何停止、启动或重启操作前完成只读准入检查。
- `POSTGRES_DB` 必须精确为 `addp_online`，并拒绝 `addp`、`addp_test`、`addp_iam_test`。该数据库只属于当前 T4 部署，不属于本地共享 PostgreSQL 测试清单。
- 证据目录必须位于仓库外；凭据目录不得被 artifact 归档。工作区必须干净，构建身份必须与当前 checkout 一致。

两种 profile 共用同一个 `make test-online` 分发器和 owner 业务断言，生命周期脚本只负责当次环境和夹具。编排只调用现有 Infra、开发生命周期脚本和 `make test-online`，不得在 workflow 中复制模块启动逻辑或业务断言。退出路径必须停止本次应用进程并报告清理结果；self-hosted Infra 可在专用 Runner 常驻，Hosted Infra 必须随 Job 销毁。

### 5.2 开关、身份与拓扑预检

每次 T4 运行至少满足：

- `ADDP_ONLINE_TEST=1`。
- 显式非默认 `ADDP_ONLINE_TEST_TENANT_ID`，禁止 Tenant 1。
- 全局唯一的 `ADDP_ONLINE_TEST_RUN_ID`；未提供时由分发器生成并贯穿全部子测试。
- 每个参与 HTTP 服务 `/health/live` 可用，且 Build ID、Git commit 或源码指纹与当前 checkout 匹配；进入业务断言前 `/health/ready` 必须返回 200。
- 测试 User、Service Principal 和 OAuth Scope 只具备场景所需的最小权限；不得使用个人会话、平台管理员 Token 或扩大生产身份权限。
- 通过 System AuthContext API 证明 principal、context、Tenant、client、token 与 Permission 事实后，才进入资源创建或注册拓扑操作。
- 同一 `suite + Run ID` 通过进程锁串行化，锁覆盖预检、业务断言和报告落盘全过程。

每个断言只验证规范定义的唯一身份链路，不在 User Token、Service Token 和直接 SQL 之间兼容回退。Repository / Migration 的直接数据库夹具属于 T2，不能替代 T4 API 授权验收。

API 消费方数据面边界由 `workbench-service-consumption` 复用同一 Business MySQL Fixture 和临时 Query Service 验收，不另建平行 suite。该场景必须分离两个真实 User Token：非管理员 Token 只负责 Service / Workbench 发布与消费，专用 Tenant Administrator Token 只负责 API 消费方与凭据生命周期。suite 通过正式 API 创建一个只授权主 Query Service 的 API 消费方，并必须同时证明：已授权服务返回 200；同 Tenant 未授权 Query Service 返回 `403 api_consumer_service_denied`；任一 `/api/v1/*` 控制面路由返回 `401 api_consumer_control_plane_denied`；删除 API 消费方后，旧凭据在 Gateway 30 秒正缓存对应的 45 秒有界收敛窗口内返回 `401 api_consumer_credential_invalid`。报告、日志和错误不得包含完整 API Key，每轮必须按捕获 ID 删除 API 消费方、其级联凭据、Data Application 和两个临时 Query Service，并验证零残留。

模块注册与恢复类 T4 必须使用正式进程入口至少证明：

1. 业务模块先启动、System 后启动时，模块先为 Alive/Not Ready，不能处理业务；System 恢复后使用同一 `instance_id` 自动进入 Ready。
2. System 与业务模块先启动、Gateway 后启动时，Gateway 在首个完整快照后进入 Ready 并建立路由。
3. System 运行中断后，Backend 在心跳失败被观测后转为 Not Ready，Worker/Scheduler 停止领取新工作，Gateway 在已有租约过期后返回 503。
4. System 恢复后所有实例无需重启即重新 Ready，Gateway 自动恢复路由，不需要前端人工刷新或二次点击。

仅启动 Probe Server 并直接调用 System 注册 API 可以验证租约与路由算法，但不能替代上述进程级就绪与恢复证据。

专用 Runner 可以通过受 `ADDP_ONLINE_HOST=1` 强制保护的精确进程参数复用正式开发构建与进程入口，以控制业务 Backend、System 和 Gateway 的真实启动顺序；该入口不得成为日常开发的第二套模块依赖路径。精确停止必须先验证受管 PID 的进程身份。每个进程阶段都必须把 Liveness、Readiness、业务门禁、实例身份和 Gateway 路由投影写入仓库外证据目录。

消费方与 Engine Instance 动态恢复类 T4 必须另外证明：全量重启后真实 User 在同一 Browser Context 首次打开 Console 原生页与模块 iframe 不需要 reload、手工刷新或二次点击；Engine Instance 经正式连接检测 API 从 `online` 转为 `offline` 再恢复 `online` 时，已打开页面自动更新，相关 Backend 与 Frontend PID 保持不变。Engine Instance 是永久身份，此类 suite 使用专用 Runner 预置的长期 Engine Fixture，只控制隔离物理端点的连通性，不得按 Run ID 创建、删除 Engine Instance 或清理墓碑。物理 Fixture 必须由 owner 的 Online 专用生命周期入口管理，要求 `ADDP_ONLINE_HOST=1`、不读取仓库内 `.env`、不复用平台 `POSTGRES_DB`，并在成功、失败和中断路径恢复连接观测后停止。

企业资源目录发布类 T4 使用同一永久 PostgreSQL Engine Instance 及其 owner 生命周期入口创建的稳定表 `public.addp_online_catalog_fixture`。专用环境必须预置可引用的 Standard Domain 和 Department；首次运行可将该永久数据源对应的 `discovered` CatalogEntry 初始化为稳定 `curated` fixture，后续运行必须在验收后恢复其编目聚合。同一 suite 必须重复执行真实 Meta 扫描并证明 fingerprint 与 CatalogEntry UUID 幂等，验证 `inventory` / `governance` 视图、治理覆盖率固定维度和精确来源身份解析；真实浏览器使用同一专用 User 登录 Console，验证覆盖率页、目录详情与 Domain / Department / Engine 名称选择器，并将浏览器 warning/error 计入失败。每轮创建的 Asset 和 AssetCategory 必须经正式 API 下架、删除并证明零残留；不得直接 SQL 清理。

Manager 平台内部产物类 T4 使用专用 Business MinIO Fixture 和永久 MinIO Engine Instance。Fixture owner 幂等写入仓库内确定性小型 LAS 与多页 PPTX 样本；suite 经 Gateway 触发真实 Meta scan，并使用扫描所得的 ResourceLocator、item ID 和 fingerprint 验证两条正式链路：`point_cloud_copc_generation` 必须由 PointCloud Runtime 从业务对象存储读取源文件、向 Manager infra MinIO 发布 COPC，并由 Manager execution 写入 `addp.lineage-facts/v1`；PPTX 预览必须由 Quick View Capability 声明 `generate_pptx_pdf`，并通过统一 action 入口首次触发 `pptx_pdf_generation`，由 Document Workflow / LibreOffice 发布多页静态 PDF，同源再次读取 Capability 只复用同一任务与结果，不创建第二次 execution。Monitor API 与真实浏览器必须展示同一业务输入和 `addp-infra://` 输出；浏览器还必须在 Data Explorer 翻到后续 PDF 页，跨过 Engine 状态周期刷新后仍保持当前页。退出路径通过 Manager 正式 API 删除两类结果与任务、验证对象不可再读取及临时资源零残留；不得直接操作数据库或 infra MinIO 清理。

限时原值访问 T4 必须使用两名不同的专用 User，覆盖 `manager/preview` 的完整流程：申请用户在 Manager 出口发起申请，另一名审批用户在 Security 审批，批准后仅申请用户在有效期内看到原值，审批用户和其他用户仍看到遮盖值。到期后不得刷新 Security 投影或调用 Security 判定，必须直接由 Manager 根据本地投影恢复遮盖。Enrollment、Assessment、AccessRequest、Exemption 聚合及不可变修订属于专用 Tenant 的长期治理与审计事实，不作为临时业务资源删除；Assessment 修订使授权立即失效的事务语义由 Security PostgreSQL T2 覆盖，禁止为了 T4 重复运行而篡改或删除不可变审计历史。

MySQL 邮箱四出口保护类 T4 使用专用 MySQL Fixture 的 `customers.email` 和专用 PostgreSQL Fixture 的固定目标表，要求当前 Tenant 已存在启用的 `email` 敏感类型、`addp.detector.email_metadata/v1` 检测绑定和 `suppress` 默认保护规则。suite 必须经 Meta 真实扫描发现字段，经 Security 正式 API 形成或复用 Enrollment 与 Assessment，并等待 `manager/preview`、`develop/query`、`service/service_execute` 和 `transfer/export` 投影全部确认。四个 Owner 都必须继续返回 5 条非敏感数据，同时在返回结构与每条记录中完全移除 `email`；返回空邮箱、原文邮箱、整个请求被拒绝或整个结果为空都不算通过。每轮临时 Query Service 和 Transfer 任务必须按捕获 ID 删除并确认零残留；Enrollment 与 Assessment 是长期治理事实，不在验收后删除。

OceanBase 消费链路 T4 使用专用 OceanBase CE MySQL 模式 Fixture 和永久 `engine_type=oceanbase` Engine Instance。Fixture 固定维护一个 5 行非空间 InnoDB watermark 源表和一个同构空目标表；suite 必须经 Meta 扫描取得两个 ResourceLocator，以同一条 bounded watermark Transfer 任务依次验证首批 5 行、源表确定性更新/新增后的 2 行增量，以及再次执行的 0 行空增量。目标采用 `upsert` 和唯一稳定键 `id`，不得把 OceanBase 降格注册为 MySQL 或为模块增加类型分支。首批与增量完成后，Manager 表预览、Develop SQL 和临时 Query Service 必须通过各自正式数据出口读取同一目标 ResourceLocator，并得到一致 checksum，最终固定为 6 行且无重复键；Manager 预览还必须确认扫描结构中的完整列顺序。临时 Transfer 任务和 Query Service 必须按捕获 ID 删除并确认 404；物理 Fixture 在成功、失败和中断退出路径恢复源表基线与空目标表后停止。该 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得增加定时触发。

openGauss 消费链路 T4 复用上述关系引擎 owner 断言，但只在 GitHub Hosted `ubuntu-24.04` x86_64 profile 上运行。生命周期从固定 SHA-256 的 openGauss 6.0.6 LTS 官方介质启动独占容器，创建 `addp_online` 平台库、非默认 Tenant、最小权限消费 User、只含 `system.engine.create/read/execute` 的 Engine Provisioner 和临时 `engine_type=opengauss` Engine Instance。Business Fixture 只管理数据库并输出 owner-only Engine 描述，不调用 System API；System IAM owner helper 只创建身份和 Token，通用 Online 注册器使用 Engine Provisioner Token 通过正式 System API 注册并验证 Engine。Fixture 以 `public` schema 作为 namespace，使用 openGauss 原生 `MERGE` 推进相同的 5/2/0 watermark 数据集；Manager、Transfer、Develop 和 Service 必须通过与 OceanBase 相同的通用 ResourceLocator、SQLDialect 和 Provider 路径得到同一 6 行 checksum，上层模块不得新增 `opengauss` 分支。当次 Engine Instance 只随 disposable 平台库销毁，Token 和连接凭据只保存在不归档的 owner-only `runner.temp` 目录；Engine 注册后必须在进入业务断言前从进程环境清除 Provisioner Token 与数据库凭据，业务证据不得包含凭据。该 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得增加定时触发。

Transfer 仅新增 MySQL T4 使用永久 PostgreSQL 与 MySQL Engine Instance，并由专属复合 Fixture 管理两端物理表。真实 User 必须从 Console 页面选择源表和目标库、进入字段映射、通过正式“分析源数据并推荐精度”接口把无声明精度的 PostgreSQL `numeric` 收敛为不会截断当前值的 MySQL `DECIMAL(6,2)`，再以主键 `id` 作为唯一 watermark 创建任务。页面自动执行的首轮必须读写 6 行；Fixture 同时修改 `id=1` 并新增 `id=7` 后，用户从任务详情再次执行必须只读写 1 行。最终 MySQL 目标必须共 7 行、`id=1` 保持首轮值、`id=7` 为新增值且无重复键，以证明单字段水位只覆盖严格递增新增、不承诺旧记录更新。浏览器还必须断言任务配置的 `tie_breaker=[]`、目标 `upsert` 键为 `id`，捕获并删除临时任务且确认 404；Fixture 在全部退出路径删除两端固定表并停止两个容器。该 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得增加定时触发。

### 5.3 数据、超时与清理

T4 临时夹具优先通过 owner 正式 API 创建；正式 API 无法建立必要前置状态时，才允许 owner 提供专用测试 helper。Hosted profile 的全新平台库在尚无可登录 User 时，可由 System-owned helper 调用正式 IAM Service 创建当次 Tenant、User、Role 和 Session；helper 必须限定 GitHub Hosted Linux 及 `addp_online`，不得通过 SQL 写入 Principal、Role、Assignment 或 Token。Engine Instance 等永久身份按上一节使用预置专用 Fixture，不适用“每轮创建后删除”；Hosted disposable profile 的当次 Engine Instance 随平台数据卷整体销毁。跨模块 Online 场景不得以直接 SQL 作为常规夹具路线。

每个 suite 必须：

- 让全部资源名称可追踪到 Run ID。
- 为总场景、单次 HTTP 请求、路由收敛和租约收敛设置明确超时。
- 只对临时传输失败或规范允许的异步收敛执行有界重试；业务冲突、权限失败和 409 不自动重试。
- 在成功、失败、超时与中断路径执行 owner 清理并检查错误。
- 结束时查询并断言残留为零；清理失败时，即使业务断言通过，门禁仍失败。
- 将故障注入放在测试 Transport、测试 Provider 或明确测试 hook，不在生产代码中保留调试分支。

### 5.4 报告

统一分发器必须在仓库外证据目录生成 `addp.online-gate/v1` 报告，成功与失败采用同一结构。报告至少包含：

- suite、scenario、Run ID、Git commit 和工作区状态。
- 参与服务的脱敏地址与构建身份。
- Tenant、数据库类别、阶段耗时和稳定错误码。
- owner suite 创建、清理和零残留证据。

报告、日志和 CI artifact 不得包含 Token、Client Secret、完整敏感响应或可复用凭据，也不得提交到仓库。

### 5.5 触发策略

新增 T4 suite 先通过手工 `workflow_dispatch` 执行。专用 Runner 至少完成一次真实通过，并确认构建身份、清理和零残留证据后，才允许增加夜间 schedule。环境未就绪必须报告环境失败，不得 Skip 为通过。

## 六、T5 发布认证协议

T5 按产品或 Runtime 独立准备真实前置条件，例如 macOS Keychain、安装包生命周期、HA、故障切换或在线厂商证据。统一 `test-release` 分发器只选择 owner 门禁并生成 `addp.release-gate/v1` 报告，不合并不同产品的运行条件。

厂商只提供离线 Docker tar、且 Runtime 受 OS/CPU 架构约束时，官方介质认证归 T5：owner 脚本必须在一处固定官方 HTTPS 地址和 SHA-256，负责下载、校验、`docker load`、原生启动、真实驱动/SQL 断言及所有退出路径清理；workflow 只选择匹配架构的 Runner 并调用统一 `test-release` 入口。此类 suite 不声明 `ADDP_T2_SERVICES`、不进入 `test-integration` 或辅助 macOS 巡检，也不能替代插件实现后的 T2 Provider 集成与 T4 跨模块验收。

发布 workflow 只准备环境、调用标准 Make 入口、归档证据和执行发布动作，不在 YAML 内重写业务测试。System IAM PostgreSQL 属于 T2，不因与发布流程共用 workflow 而变成 T5。

未具备真实凭据或 Runtime 的 T5 suite 不得登记占位实现；需要人工认证时必须明确记录未验证项和后续责任门禁。

## 七、CI 编排与登记

Workflow 只负责：

- 触发条件与 owner 影响选择。
- Runner、语言版本和 disposable Service 准备。
- 调用唯一 Make / script 入口。
- 超时、并发、Artifact、Step Summary 和 required check 名称。

不得在 workflow 中复制 SQL、业务夹具、测试选择表达式、模块启动逻辑或清理逻辑。能通过 Git 和依赖声明自动发现的事实不维护手写清单；必须手工登记的门禁由 `make test-platform` 的一致性检查验证完整性。凡通过 `ADDP_T2_SERVICES` 声明 PostgreSQL、MongoDB、MySQL、OceanBase 或后续数据库 Service 的 T2 门禁，都必须同时登记根 Make 入口、`test-integration` 串行聚合、共享模块变更选择和带摘要的 CI Job；每个声明的 Service 必须存在，并使用显式版本 tag 与镜像 sha256 digest。T5 suite 如声明 `workflow_job`，登记检查必须通用验证该 Job 只经 `test-release` 分发、配置仓库外证据目录并挂接统一摘要，不按产品或数据库类型增加检查分支。

新增或修改模块、测试入口、基础设施依赖、构建方式或 suite 时，必须在同一次变更中同步：

1. owner 测试与安全检查。
2. 根 `Makefile` 标准入口。
3. 自动发现、影响选择或显式 suite 登记。
4. `.github/workflows/` 编排与报告。
5. 对应规范、模块说明或运行指南。

不得提交一个永久排队、连接开发环境或始终 Skip 的占位 workflow。

### 7.1 辅助 macOS 定时巡检

日常使用的 macOS 可以在独立 checkout 中定时运行 `make local-ci`，作为 GitHub Actions 之外的辅助反馈机制。该入口只复用已有 T0-T3 标准门禁，不产生第二套测试事实，也不替代 GitHub required checks。

- checkout 必须专用于自动巡检，处于 `main`，且不得包含已跟踪或未跟踪改动；脚本只使用 `git fetch` 与 fast-forward，不使用 reset 或 clean 覆盖现场。
- 首次运行执行全部确定性门禁和已登记基础设施集成门禁；之后以上次成功 SHA 为 `BASE_REF` 运行 `make test-changed`。每个新 SHA 同时执行根 `make build BUILD_ARGS=--force`，复验全部 Linux 产品二进制。失败不推进成功基线，后续调度必须重试同一提交。
- 测试范围与远端同步是两个正交选择：默认执行“同步 `origin/main` + 增量”，`--full` 强制全量，`--no-fetch` 跳过 `fetch` 与 fast-forward；专用机需要在当前干净 `main` 上执行不拉取的完整巡检时，统一使用 `make local-ci LOCAL_CI_ARGS="--no-fetch --full"`。`--check-only` 是独立的就绪检查，不与其他选项组合。运行日志和 `latest-summary.txt` 必须分别记录实际 `scope` 与 `remote_sync`，不能把“不拉取”误报成“增量”。
- Python 与前端依赖准备只是环境编排；真实断言仍由根 `Makefile` 与 owner 门禁拥有。
- 本地 PostgreSQL 继续只使用 `addp_test` 与 `addp_iam_test`，集成门禁严格串行。持久基础设施只启停 `addp-infra` Compose 项目；巡检专属 MySQL 和 OceanBase 由唯一的 `addp-local-macos-ci` Compose 项目管理，不操作其他 Compose 项目。脚本不执行 Docker 全局 prune，也不删除 `addp-infra` 数据卷。
- MySQL owner 门禁使用本地巡检专属、固定版本且无数据卷的 MySQL 8 服务。该服务只绑定 `127.0.0.1:13306`，必须在确定性门禁和编译前通过健康检查；外层聚合门禁只携带本地作用域标记，真实连接参数只在 `test-common-mysql-data-protection` 进程内生成，不能写入仓库根 `.env`、传给其他 owner 门禁或连接 Business/生产 MySQL。
- OceanBase owner 门禁使用本地巡检专属、固定 digest 且无数据卷的 OceanBase CE 4.4.2 LTS 服务。该服务只绑定 `127.0.0.1:12881`，并且必须在确定性门禁和编译前通过健康检查；外层聚合门禁只携带本地作用域标记，`common-oceanbase-gate.sh` 在该标记下固定生成 `addp_oceanbase_disposable` 连接参数。巡检启动前必须清除继承的 `ADDP_TEST_OCEANBASE_*`，不读取根 `.env`，不连接 Business/生产 OceanBase。
- 两个巡检专属数据库服务都必须在巡检成功、失败和中断的统一退出清理中删除。
- 辅助巡检不运行 T4 Online 或 T5 发布认证；两者继续遵循各自的专用环境、身份与报告协议。
- 日志、上次成功 SHA 与运行锁位于 checkout 的 Git 内部状态目录，不进入工作树；日志不得包含 Token 或可复用凭据。

## 八、交付与完成标准

实施前识别受影响的 T0-T5 层级。交付前优先运行：

```bash
make test-changed
```

验证指定 owner 或已提交区间时分别使用 `make test-module MODULE=<module>`、`make test-changed BASE_REF=<ref>`。受环境限制无法运行时，必须列出未验证项、原因以及负责验证的现有 CI 门禁。

测试体系变更完成必须满足：

1. 新逻辑只有一条标准入口，旧入口、变量和旁路已删除。
2. owner、测试层级、数据与身份边界明确。
3. 本地标准入口、CI 编排、登记检查和文档同步。
4. 最小充分门禁真实执行，未以 Skip、旧进程或错误环境冒充通过。
5. 运行产物位于操作系统临时目录或 CI artifact，仓库无残留。
