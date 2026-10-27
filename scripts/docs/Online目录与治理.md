# ADDP 脚本：Online目录与治理

[返回脚本导航](../README.md)

本页导航：[标准与模型引用删除](#标准与模型引用删除) · [企业目录发布](#企业目录发布) · [质量动态绑定](#质量动态绑定)

测试分层、身份和清理规则以[测试与验收规范](../../docs/spec/addp测试与验收规范.md)为准；历史结果仅适用于所列源码和运行尝试，不代表当前 HEAD 已重跑。

## 标准与模型引用删除

首个登记项为 `standard-model-reference-deletion`。它要求 `GATEWAY_URL`、`SYSTEM_URL`、`STANDARD_URL`、`MODEL_URL`、显式测试 Tenant 和 `ADDP_ONLINE_TEST_USER_ACCESS_TOKEN`；Token 对应专用测试 User。套件先通过 System AuthContext 确认 User、Tenant 和 User Access Token 身份，拒绝平台或租户管理员角色，并验证 Standard Domain 和 Model Entity 的 create/read/delete Permission。业务创建、读取和删除请求全部经 Gateway 转发，直连服务地址只用于构建身份预检。场景创建 Standard Domain 和引用它的 Model Entity，断言 Domain 删除返回 `409 standard_resource_referenced`，删除 Entity 后再删除 Domain，最终通过双方 GET 404 证明零残留；任何身份、业务或清理错误均使门禁失败。

## 企业目录发布

`enterprise-catalog-publishing` 要求全量 System、Gateway、Meta、Catalog、Asset、Portal 和 Console，并复用专用 PostgreSQL Engine Fixture。Fixture owner 在物理库启动时幂等建立 `public.addp_online_catalog_fixture`；suite 只经 Gateway 使用真实 User Token 连续发起两次 Meta 扫描，证明 fingerprint 与 CatalogEntry UUID 幂等，再验收资源盘点/治理目录视图、七维治理覆盖率、精确来源身份解析和业务编目。随后以 `AssetComponent.catalog_entry_id` 创建并发布资产，从 Portal 校验同一 CatalogEntry 身份、AssetCategory 目录树节点与分类子树资产列表；真实浏览器使用同一专用 User 登录，先验证 Catalog 覆盖率、详情、名称导航和批量治理对话框，再打开 `/portal/categories/:id` 确认临时目录节点与 Asset 卡片，浏览器不实际提交第二次治理写入。临时 Asset 按 `published → offline → deleted` 清理，suite 同时确认空分类立即从 Portal 目录树消失，再删除 Asset-owned 目录；已初始化的永久 Catalog fixture 会恢复运行前编目聚合。环境另需 `ADDP_ONLINE_TEST_USER_USERNAME`、`ADDP_ONLINE_TEST_USER_PASSWORD`、`ADDP_ONLINE_TEST_CATALOG_DOMAIN_ID` 和 `ADDP_ONLINE_TEST_CATALOG_DEPARTMENT_ID`。

### 企业资源目录专用 macOS 完整验证

复用现有两类标准入口，不建立 `test-all` 或另一套目录脚本。T0-T3 与 T4 的身份和基础设施边界不同，应使用同一台专用 macOS 上的两个独立干净 checkout，或分别由 Local CI checkout 与 `addp-online` GitHub Runner checkout 执行：

| 案例 | 层级 | 标准入口 | 通过证据 |
| --- | --- | --- | --- |
| `ECV-00` 验证机准入 | T0/T4 preflight | `make local-ci LOCAL_CI_ARGS=--check-only`；`online-host-gate.sh --check-only` | 工具链、干净 checkout、外部环境文件、独立证据目录、`addp_online` 与 loopback 边界通过 |
| `ECV-01` 全量确定性门禁 | T0-T3 | `make local-ci LOCAL_CI_ARGS=--full` | 全仓 T0-T1、全部已登记 PostgreSQL T2、前端 T3、全部 Linux 产品构建通过 |
| `ECV-02` 构建与身份 | T4 | `enterprise-catalog-publishing` | System/Gateway/Meta/Catalog/Asset/Portal Build Identity 与 checkout 一致；User/Tenant/Permission 验证通过 |
| `ECV-03` 自动建档幂等 | T4 | 同上 | 两次真实 Meta scan 返回成功，fingerprint 与 CatalogEntry UUID 均不变化 |
| `ECV-04` 目录读模型 | T4 | 同上 | 同一条目进入 `inventory` 与编目后的 `governance` 视图；治理状态总数与七个覆盖率维度分母自洽 |
| `ECV-05` 动态来源解析 | T4 | 同上 | Meta fingerprint 经 `POST /catalog/entries/resolve-sources` 精确解析到当前 active CatalogEntry |
| `ECV-06` Catalog 与 Portal 交互 | T4 browser | 同上 | 覆盖率七维、CatalogEntry 详情、名称选择器、当前页显式多选和批量治理对话框可用；Portal 分类页展示临时目录与唯一 Asset 卡片，页面不出现 `undefined`，浏览器 warning/error 与失败业务响应均为 0 |
| `ECV-07` 发布消费唯一路线 | T4 | 同上 | `Meta → Catalog → AssetComponent → Portal` 保持同一 CatalogEntry UUID；AssetCategory 出现在 Portal 目录树，分类子树只返回该临时 Asset |
| `ECV-08` 清理 | T4 | 同上 | 临时 Asset 下架后删除，空分类从 Portal 树消失，Asset-owned 目录删除、Portal Asset 404，`residual_resources=0`，永久 fixture 完整编目聚合重新读取并核对恢复 |
| `ECV-09` 弃用责任与恢复 | T4 | 同上 | 专用 fixture 弃用后，经责任子资源增减可选技术负责人但保持弃用与其他冻结事实；撤销弃用只回到已编目；两类旧版本写入返回规范 409 且无副作用；成功和失败后恢复责任及编目信息 |
| `ECV-10` 正式共享受理 | T4 | 同上 | 真实 User 业务确认→Catalog 提交→System 专用 OAuth 反查→首次受理；同编号换用途／批准要求版本须 409，拒绝后原决定与回执不变；同参重放完整绑定、受理时间与五分钟截止不变，人类 Token 在两个机器接口均返回 403；回执不是 Grant |
| `ECV-11` 自动签发与历史恢复 | T4 | 同上 | 正式 Catalog 消费者自动签发，System owner 测试夹具在只读一致快照中核对精确 Tenant、Engine 和请求的唯一 Grant 与唯一成功签发审计；原参重试后签发时间及审计次数不变；人类访问签发和历史机器接口均为 403；不代表执行侧允许读取数据 |

`ECV-09` 复用当前专用 User、Domain 和 Department，不创建第二个身份或自动赋权。测试 User 除原有读取、盘点和编目维护权限外，必须显式具备 `catalog.entry.deprecate`，缺失时身份预检直接失败。责任变化只验证同账号的可选技术负责人配置；跨部门可发现范围、权限交集及审计原子性由现有 Catalog PostgreSQL T2 覆盖。脚本的故障注入及完整恢复断言复用 `make test-online-runner`，仍由 Platform CI 和现有 `Online T4 gates` 的 `enterprise-catalog-publishing` 执行，不另建 suite 或 workflow。

`ECV-10` 也复用该专用 User，要求其为永久 fixture 的当前 `business_owner`，管理员须预置有效引擎管理委派，并显式授予 `catalog.sharing_decision.create`、`system.engine_access_fulfillment.create`、`system.engine_access_approval_requirement.initialize/read`。责任身份不代替 Permission，脚本不创建委派、不改 Role 或自动移交业务负责人。专用 Runner 的仓库外配置另需独立 `SYSTEM_SERVICE_CLIENT_SECRET`，缺失在启动生命周期前失败；普通 ADDP 部署仍允许该配置留空。机器 Secret 只由正式服务使用，脚本不代签 Service Token 或借用 User Token 反查。精确表的批准要求若不存在，首次通过正式 API 初始化为 `catalog`；若已采用 `independent` 模式则停止，不覆盖既有选择。共享显式到期十分钟，同参重放不得续期；调用方丢弃首次响应后重放原三个输入，仅验证调用方恢复边界，不等同双服务之间断网或进程重启。主报告当前为 `addp.enterprise-catalog-publishing/v4`，浏览器协议仍为原 v2；`catalog_sharing` 单列不可变决定、已核清请求和 System 回执，作为长期审计事实保留，不计入临时 Asset／AssetCategory 的残留。`pending` 或核清失败必须使门禁失败，不能删库解除保护。不同人员、跨 Tenant、撤权及锁竞争继续由既有 T2 验证；自动 Grant 写入由下述 ECV-11 核对，执行侧内容读取和双服务跨进程故障仍另行验收，不能据此报告数据授权闭环已完成。

`ECV-11` 沿用 ECV-10 的真实 User 和正式服务身份，不给脚本签发机器 Token。只读核对复用 System 的 `cmd/online-test-fixture --suite enterprise-catalog-publishing --observe-catalog-grant <请求 UUID>`，只允许显式 `ADDP_ONLINE_HOST=1`、`ADDP_ONLINE_TEST=1`、回环 PostgreSQL 和专用 `addp_online` 数据库，Tenant／Engine 来自已准入环境。该模式不执行身份初始化、迁移、写入或通用 SQL，只读取 System 自有受理、签发和审计表。签发未收敛、查询错误、非唯一审计或重试改变历史均失败，不把受理成功当作签发成功；主报告为 v4，保留每次唯一签发审计，不算临时发布资源残留。确定性测试仍由 `make test-online-runner` 自动覆盖；真实 T4 沿用现有手工 suite，不增加定时触发或接管开发服务。

观察器的 PostgreSQL 查询和只读事务拒写测试也纳入现有 `make test-system-iam-postgres` 全量包发现；聚焦开发验证可用同一标准脚本 `bash scripts/test/system-iam-postgres-gate.sh --package online-fixture`。它仍先核对显式测试 DSN 和共享互斥锁，只使用允许的测试 database，重建并清理 System 测试 Schema，不连接开发库。

T0-T3 checkout 执行：

```bash
make local-ci LOCAL_CI_ARGS=--check-only
make local-ci LOCAL_CI_ARGS=--full
```

T4 推荐从 GitHub Actions 手工选择 `Online T4 gates / enterprise-catalog-publishing`。需要在 Runner 上直接预检时，只从调用方提供 suite 和仓库外证据目录，Secret 仍全部来自仓库外环境文件：

```bash
ADDP_ONLINE_HOST=1 \
ADDP_ONLINE_ENV_FILE=/absolute/path/to/addp-online.env \
ADDP_ONLINE_ARTIFACT_DIR=/absolute/path/to/evidence \
ONLINE_SUITE=enterprise-catalog-publishing \
bash scripts/test/online-host-gate.sh --check-only
```

预检通过后去掉 `--check-only` 即执行正式生命周期和唯一 `make test-online` 入口。验收必须同时保留 `readiness.txt`、`summary.txt`、`online-report.json`、`enterprise-catalog-publishing-browser.json`、`online-gate.log`、Playwright 失败截图（如有）和服务日志；任何缺失、Skip、清理失败或证据中的构建身份不一致都按失败处理。

## 质量动态绑定

`quality-dynamic-binding` 使用 GitHub Hosted Linux 一次性部署；唯一准备入口为 `scripts/test/online-hosted-quality-gate.sh`，不再接受自托管 Runner 的永久夹具配置。共享 Hosted 准入要求干净工作区、仓库外独立证据与凭据目录、无既存 Infra 或业务源。脚本启动 System、Gateway、Meta、Standard、Model、Quality Worker、Orchestrator 和 Monitor，通过 System owner helper 分别建立普通执行 User、夹具准备 User、引擎注册 User 及独立 Monitor-only 反例 User。准备身份通过正式 API 创建和审批两个同结构 Model 逻辑表，由 Model 物化接口创建带所有权标记的目标；仅在当次业务 PostgreSQL 写入固定的非零数据，再由 Meta 扫描结构。`ADDP_ONLINE_QUALITY_FIXTURES_JSON` 由准备流程生成，仍严格只含两个对象的 `logical_table_id`、规范 `locator` 和正整数 `row_count`。执行身份不授予 Model 定义创建、审批或引擎管理权限；准备凭据在业务验收前移除。

入口为 `make test-online ONLINE_SUITE=quality-dynamic-binding`；在 `Online T4 gates` 手工选择同名 suite，首次真实通过前不加入 schedule。确定性回归及注册一致性检查使用 `make test-online-runner`，并由 `make test-platform` 和 Platform CI 覆盖。脚本使用正式 owner API 创建带 Run ID 的两条规则、两个方案、两个编排；依次验证失败、重复失败、另一目标失败、显式 R2 升级后逐目标恢复，以及默认绑定、工单和质量概览隔离。通过 Monitor 校验 Quality 的开始、进度和终态事件，限制响应字段且不输出专业结果或执行授权信息。独立 Monitor-only 身份必须被 Quality 专业列表及详情拒绝，并不能读取相应 Monitor 详情或事件；删除方案后，获授权的执行身份仍能读取已保存的方案历史。只读取物理数据，不执行 ETL；Model 任务只校验预置目标结构。

仓库外 `quality-fixture.json` 记录准备身份、资源与执行标识，`quality-dynamic-binding.json` 记录业务断言和清理状态，均不记录 Token 或数据正文。套件运行期间 Model 定义与物理数据保持不变，结束时校验源数据与固定基准一致。业务套件按逆序删除本轮编排、方案和规则并确认 404；Hosted 退出统一停止本轮应用、销毁带归属标签的业务源和 Infra、删除凭据目录并核对零残留。失败、信号中断、创建响应丢失、数据变化或清理不可核验均不能计为通过。保留 `readiness.txt`、`summary.txt`、`online-report.json`、两个 Quality 报告和运行日志；原始执行历史随本轮 Infra 销毁。HTTP 最多 30 秒、单次编排等待 120 秒、业务场景 600 秒。确定性最小入口为 `make test-quality-online-runner`，由 `make test-online-runner` 和 Platform CI 自动覆盖。
