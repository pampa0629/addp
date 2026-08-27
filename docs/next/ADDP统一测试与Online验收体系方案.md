# ADDP 统一测试与 Online 验收体系方案

> 状态：稳定规则已迁入正式规范；首次 `module-registry-recovery` T4 已触发但仍在等待专用 Runner 接单（2026-08-26）。

## 一、专题边界

测试分层、标准入口、数据与身份隔离、CI 编排以及 Online / Release 报告的稳定规则，统一以 [ADDP 测试与验收规范](../spec/addp测试与验收规范.md) 为准。

当前实现事实由以下入口拥有：

- 根目录 `Makefile`：公共测试命令。
- `scripts/README.md`：可用目标、参数和运行说明。
- `scripts/test/`：自动发现、suite 登记、安全预检、分发和报告。
- `.github/workflows/`：CI 触发、Runner、环境和证据归档。

本文不再复制上述规范和实现，只跟踪尚未具备硬件条件的 T4 实机事项。待完成关闭条件后删除本文，不归档第二份历史规范。

## 二、已完成基线

- [x] T0-T5 分层、根 `Makefile` 单一入口和 owner 边界已形成正式规范。
- [x] `make test-changed`、`make test-module` 与 PR T0-T3 使用同一 owner 影响计算。
- [x] `make test-integration` 严格串行编排全部已登记的 disposable 基础设施门禁。
- [x] `make test-online ONLINE_SUITE=<suite>` 统一 Run ID、预检、超时、进程锁和 `addp.online-gate/v1` 报告。
- [x] `standard-model-reference-deletion`、`module-registry-recovery`、`consumer-engine-recovery`、`enterprise-catalog-publishing` 与 `workbench-service-consumption` 已登记为真实 T4 suite，不存在占位 suite。
- [x] 专用宿主机门禁会在生命周期操作前检查 macOS、`ADDP_ONLINE_HOST=1`、仓库外环境文件与证据目录、干净 checkout、`addp_online` 数据库和 suite profile。
- [x] 手工 `.github/workflows/online-t4-gates.yml` 已绑定 `addp-online` Environment，并使用 `self-hosted`、`macOS`、`addp-online` Runner 标签。
- [x] 手工 Online T4 workflow 已进入远端并被 GitHub 识别为 active；workflow 语法和 Runner 上下文约束由 `make test-platform` 自动检查。
- [x] 宿主机编排复用正式 Infra / Dev 生命周期与 `make test-online`，退出时停止应用并输出清理证据。
- [x] `make test-release RELEASE_SUITE=<suite>` 与 `addp.release-gate/v1` 已统一现有 T5 产品门禁调用和报告。

## 三、专用 T4 Runner 首跑与消费方恢复场景

2026-08-26 已对 `main` 提交 `0f5a88e4ff8c6a7cd364da401050cfd2c6806d10` 触发 [Online T4 run 32915692407](https://github.com/pampa0629/addp/actions/runs/32915692407)，输入为 `module-registry-recovery`。Workflow 和 Job 已正确创建，但持续为 `queued` 且没有执行步骤，说明尚未被匹配 `self-hosted`、`macOS`、`addp-online` 标签的 Runner 接单。该记录不算首次真实执行；恢复 Runner 后应继续观察现有运行，避免重复触发并制造并发排队。

排查事实：Job 返回的 `runner_name`、`runner_group_name` 为空，标签声明与 workflow 一致；当前开发 Mac 未发现 Runner 进程、launchd 服务或安装目录，且个人 checkout 因根 `.env` 和脏工作区不满足 Host Gate。当前阻塞属于专用部署资源未在线或尚未准备，不属于业务代码、suite 或 workflow 故障。不得删除 `addp-online` 标签、改用 GitHub-hosted Runner 或放宽环境隔离规则规避该阻塞。

### 3.1 机器与环境准备

- [ ] 准备独立 macOS Runner 账号和独立 checkout，并注册 `self-hosted`、`macOS`、`addp-online` 标签。
- [ ] 创建 `addp-online` GitHub Environment，将仓库外环境文件的绝对路径配置为 `ADDP_ONLINE_ENV_FILE` 变量。
- [ ] 环境文件基于当前部署配置准备专用测试 Tenant、最小权限 User / Service Principal、`POSTGRES_DB=addp_online`，以及五个 suite 所需的服务地址、浏览器用户凭据、PostgreSQL/MySQL Engine Fixture、永久 Standard Domain 和 Department ID。
- [ ] 确认 Runner 不存在仓库根 `.env`，不连接个人开发服务、`addp`、`addp_test` 或 `addp_iam_test`。
- [ ] 确认证据目录、Docker、Go、Node、npm、Python、curl 和可用磁盘满足宿主机门禁。

### 3.2 首次真实执行

通过手工 workflow 分别运行：

1. `module-registry-recovery`。
2. `standard-model-reference-deletion`。
3. `consumer-engine-recovery`。
4. `enterprise-catalog-publishing`。
5. `workbench-service-consumption`。

每次运行必须核验：

- [ ] 宿主机只读准入在任何服务停止或启动前通过。
- [ ] 被测服务健康且构建身份等于 workflow checkout。
- [ ] 测试 Tenant、AuthContext、Principal 与 Permission 满足 suite 的精确约束。
- [ ] `online-report.json` 使用 `addp.online-gate/v1`，包含阶段、构建身份、清理和零残留证据，且不含秘密。
- [ ] workflow Artifact 同时保留宿主机 readiness、gate summary、业务报告和必要日志。
- [ ] 成功、失败或中断后应用进程均停止，下一次执行不受上次残留影响。

### 3.3 进程乱序恢复证据

`module-registry-recovery` 已验证注册、租约、同 ID 恢复、多实例与 Gateway 路由收敛；仍需由专用部署编排补充真实进程启动次序证据：

- [ ] 模块先启动、System 后启动时，公共注册客户端自动恢复。
- [ ] System 与模块先启动、Gateway 后启动时，路由发现自动收敛。
- [ ] 全部服务重启后，Manager、Service 等消费方首次打开不依赖人工刷新。
- [ ] 引擎或模块恢复在线后，消费方无需重启即可动态恢复使用。

该编排必须复用正式进程入口和专用 T4 环境，不得在个人开发环境人工操作后宣称自动化通过。

### 3.4 `consumer-engine-recovery` 实现契约

该独立 suite 负责补齐上节最后两项，不扩大 `module-registry-recovery` 的边界：

1. Host Gate 使用正式全量启动入口，保留 Manager、Service Backend 和三个被测 Frontend 的初始 PID。
2. 专用 Runner 环境预置一个长期存在的 PostgreSQL Engine Instance；仓库外环境文件提供 `ADDP_ONLINE_TEST_ENGINE_ID`、`ADDP_ONLINE_TEST_ENGINE_NAME`、`ADDP_ONLINE_TEST_ENGINE_PORT`、`ADDP_ONLINE_TEST_ENGINE_USER`、`ADDP_ONLINE_TEST_ENGINE_PASSWORD` 和 `ADDP_ONLINE_TEST_ENGINE_DATABASE`。这些变量只属于 Online 业务 Fixture，不得复用平台数据库变量。
3. `business/scripts/online-engine-fixture.sh` 是该物理 Fixture 的唯一生命周期入口，要求 `ADDP_ONLINE_HOST=1`，不读取、不生成 `business/.env`，并只操作 `business-postgres`。
4. Browser 使用 `ADDP_ONLINE_TEST_USER_USERNAME` 与 `ADDP_ONLINE_TEST_USER_PASSWORD` 走真实 Console 登录；API 控制面继续使用同一专用 User 的 `ADDP_ONLINE_TEST_USER_ACCESS_TOKEN`。测试必须确认两种会话属于同一 Tenant User 且具备 `system.engine.read`、`system.engine.execute` 和被测页面的读取权限。
5. 同一 Browser Context 依次首次打开 `/configuration`、`/manager/data-explorer` 和 `/service/query-services`，每页只允许一次导航；页面自身的首次请求成功即为通过，不允许 reload、刷新按钮或失败后重试导航。
6. Manager iframe 保持打开时停止 Fixture、调用 Engine test API 写入 `offline`、等待页面轮询显示不可用；恢复 Fixture 并再次写入 `online` 后，同一 iframe 自动恢复。整个阶段被测消费方 PID 不变。
7. `finally` 路径必须恢复 Fixture 与 Engine `online` 状态；Host Gate 随后停止 Fixture 和全套 ADDP，并把浏览器报告、截图、PID 快照和 Online JSON 报告统一写入仓库外 Artifact 目录。

Engine Instance 是永久身份，因此 suite 禁止按 Run ID 创建后删除测试引擎，也禁止以墓碑、名称前缀清理或直接修改 System 数据库作为收尾方式。

2026-08-26 已完成 suite、浏览器断言、物理 Fixture、进程稳定性观测、Host Gate 失败恢复、workflow choice 和 CI 登记检查；本地确定性测试与三个前端构建已通过。该状态只表示实现就绪，不表示专用 Runner T4 已通过。

### 3.5 `enterprise-catalog-publishing` 实现契约

1. Host Gate 全量启动 System、Gateway、Meta、Catalog、Asset 和 Portal，并通过 `business/scripts/online-engine-fixture.sh` 幂等准备 `public.addp_online_catalog_fixture`。
2. 真实 User Access Token 先经 System AuthContext 验证 Tenant 和精确 Permission，所有业务请求只经 Gateway，不直连 owner 或直接 SQL。
3. suite 触发 Meta 扫描，以 DataItem fingerprint 等待唯一 CatalogEntry 自动建档；首跑可使用预置 Domain、Department 和当前 User 将永久 fixture 初始化为 `curated`。
4. 后续运行对同一 CatalogEntry 执行临时编目并在 `finally` 恢复原完整聚合；以 CatalogEntry UUID 创建 AssetComponent，发布后由 Portal 返回同一身份。
5. 每轮 Asset 经 `published → offline → deleted` 正式生命周期清理，Asset-owned 目录随后删除，双方 GET 404 与 Portal 404 共同证明零临时残留。

2026-08-26 已完成 owner suite、永久数据源 fixture、Asset 下架后删除闭环、Host Gate profile、workflow choice、CI 登记检查和确定性协议测试。当前仍是“实现就绪、专用 Runner 首跑待执行”。

### 3.6 `workbench-service-consumption` 实现契约

1. Host Gate 全量启动 System、Gateway、Service、Workbench 和 Console；`business/scripts/online-workbench-mysql-fixture.sh` 使用外部环境变量启动 `business-mysql`，重建仓库既有 `customers + orders` 确定性数据并创建仅有 `SELECT` 的专用账号，同时为 Playwright 安装专用 Chromium。
2. 专用环境长期预置指向该账号的 MySQL Engine Instance，身份由 `ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID` 提供；suite 不创建、修改或删除 Engine Instance。
3. 真实 User Token 先校验 Tenant、非管理员角色及 Service/Workbench 最小权限，随后所有平台操作只经 Gateway：检测 SQL 输出契约、发布私有 Query Service、读取 Consumer Descriptor、创建 Workbench View 和执行查询。
4. 临时 Query Service 固定名为 `commerce-order-analysis`，SQL 只 JOIN 既有表并排除姓名、邮箱、电话和地址；MySQL 将 `BOOLEAN` 暴露为 `TINYINT`，publisher 只把已知语义的 `active_customer` 显式发布为 `bool`，Service 再按冻结契约归一化 `0 | 1`；验证 `order_no` cursor、五类动态筛选、decimal/boolean/timestamp/null、完整有限 CSV，以及无 SpatialInfo 时 Map 不可用。
5. 浏览器使用同一专用 User 登录 Console 并核对浏览器 AuthContext 与 API User 身份一致，打开保存的 View 后实际执行动态状态参数，验证 Table 两行结果、Chart canvas 和无空间契约下不存在 Map 选项。
6. 浏览器经正式 Service API 更新公开默认字段策略以改变 `contract_fingerprint`；刷新同一 View 后必须显示契约变化告警并禁用查询。API 层同时确认已保存 View 仍持有旧指纹，不增加 Workbench 执行 API 或测试旁路。
7. `finally` 只按本轮捕获的 View UUID 和 Query Service ID 删除临时资源并逐项验证 404；Fixture 和全套 ADDP 再由 Host Gate 统一停止。

2026-08-26 已完成 owner suite、浏览器 Table/Chart/Map/契约变化链路、只读 MySQL Fixture、Host Gate profile、workflow choice、CI 登记检查和确定性协议测试。当前仍是“实现就绪、专用 Runner 首跑待执行”，不得勾选 Workbench 专题中的真实 Online 验收项。

## 四、关闭条件

满足以下条件后删除本文：

1. 五个已登记 T4 suite 在专用 Runner 至少各真实通过一次。
2. 构建身份、专用 Tenant、数据库隔离、失败清理和零残留证据均已核验。
3. 进程乱序恢复形成可重复的部署编排证据。
4. 根据首跑耗时与稳定性决定是否增加夜间 schedule；不需要定时执行时也必须明确记录决定。
5. `scripts/README.md`、workflow 与相关模块专题已更新为最终运行事实。

首跑日志和报告保留在 GitHub Actions Artifact 或外部证据存储中，不复制进长期规范。专题删除后，稳定规则继续由 [ADDP 测试与验收规范](../spec/addp测试与验收规范.md) 唯一维护。
