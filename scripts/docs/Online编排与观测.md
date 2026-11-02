# ADDP 脚本：Online编排与观测

[返回脚本导航](../README.md)

本页导航：[Online 统一入口](#online-统一入口) · [预检与回归](#预检与确定性回归) · [报告与证据](#报告与证据) · [部署 profile](#部署-profile-与触发) · [Compose 公开地址专项](#compose-公开地址专项) · [macOS 专用环境](#macos-专用环境字段) · [模块实例运行日志验收](#模块实例运行日志验收) · [平台节点指标 Hosted 验收](#平台节点指标-hosted-验收)

测试分层、身份和清理规则以[测试与验收规范](../../docs/spec/addp测试与验收规范.md)为准；历史结果仅适用于所列源码和运行尝试，不代表当前 HEAD 已重跑。

## Online 统一入口

Online 唯一入口为 `make test-online ONLINE_SUITE=<suite>`，并要求环境中显式设置 `ADDP_ONLINE_TEST=1`。`scripts/test/online-gate.py` 只接受代码中已登记且已有 owner 门禁实现的 suite；未登记名称直接失败，不以占位场景冒充验收。分发器为整次运行生成或传递统一 Run ID，依次执行通用预检和 owner 门禁，并对二者施加总超时。

## 预检、报告与部署隔离

### 预检与确定性回归

通用预检由分发器向[预检器](../test/online-preflight.py)传入参与服务的 `module=http://loopback:port`。Tenant、Run ID、构建身份与 Ready 的检查沿[开关、身份与拓扑预检](../../docs/spec/testing/T4-Online验收规范.md#开关身份与拓扑预检)，干净 checkout、专用 `POSTGRES_DB=addp_online` 和回环地址要求沿[专用环境](../../docs/spec/testing/T4-Online验收规范.md#专用环境)。

宿主机 `--check-only` 在生命周期操作前调用同一预检器的 `--environment-only`，不另设数据库或 Tenant 判定；该阶段不代表服务健康或业务断言已通过。分发器与预检器的无外部服务回归统一使用 `make test-online-runner`，并已纳入 `make test-platform`。两者不执行未登记的业务断言，不读取或保存 Token，也不接管服务生命周期。

### 报告与证据

分发器对同一 `suite + Run ID` 使用操作系统临时目录进程锁，锁覆盖预检、场景和报告写入。成功或失败均生成 `addp.online-gate/v1` 的 `online-report.json`：专用 Runner 写入仓库外 `ADDP_ONLINE_ARTIFACT_DIR` 并由 workflow 归档，在已准入的专用环境直接执行而未指定该目录时写入操作系统临时目录；这一落盘行为不授权复用个人开发部署。

报告字段与脱敏要求统一见[报告规范](../../docs/spec/testing/T4-Online验收规范.md#报告)。owner 的业务报告与外层部署清理结果分别认定，清理失败不得因业务断言成功而计为通过，见[数据、超时与清理](../../docs/spec/testing/T4-Online验收规范.md#数据超时与清理)。

### 部署 profile 与触发

部署选择、Runner 标签与 suite 路由统一见[T4 专用环境](../../docs/spec/testing/T4-Online验收规范.md#专用环境)，实际登记以[Online registry](../test/online-gate.py)、[workflow](../../.github/workflows/online-t4-gates.yml)和当前 Host Gate 为准。手工 `workflow_dispatch` 与已毕业 suite 的定时准入遵循[触发策略](../../docs/spec/testing/T4-Online验收规范.md#触发策略)：仅声明 `nightly=True` 的 suite 接受每日 `20:30 UTC` 的 `schedule`，其他 Job 显式排除定时事件。

| 部署 profile | 生命周期入口与专用准备 |
| --- | --- |
| macOS self-hosted | [Host Gate](../test/online-host-gate.sh)从 `ADDP_ONLINE_ENV_FILE` 加载仓库外专用环境；专用字段见下文 |
| GitHub Hosted Linux x86_64 | 各 suite 的 Host Gate 复用[Hosted 生命周期](../utils/hosted-online.sh)，从零建立当次环境；限时原值访问的双用户准备见[原值访问身份与清理](Online数据保护.md#原值访问身份与清理) |
| 当前 KingbaseES owner-managed | [KingbaseES Host Gate](../test/online-owner-managed-kingbase-gate.sh)限定 `self-hosted`、`Linux`、`X64`、`addp-kingbase` 标签及 `addp-kingbase` Environment 审批，只从 owner-only `ADDP_KINGBASE_GATE_ENV_FILE` 加载 License 路径、License SHA-256 等受控事实 |

生命周期操作前的仓库外 artifact／secret 分区、无根 `.env`、无容器冲突和干净 checkout 准入沿[专用环境](../../docs/spec/testing/T4-Online验收规范.md#专用环境)。各 Host Gate 产出不含密钥内容的 `readiness.txt`，共用唯一 `make test-online` 分发入口；常驻 macOS Infra 与 Hosted／KingbaseES 当次部署的销毁范围分别沿该规范，不混用准备或清理假设。

[Online CI 登记检查](../ci/check-online-ci-registration.py)要求 suite、部署 profile、Runner 预检、workflow choices 与夜间登记完全一致，拒绝未毕业的定时 suite、非每日 cron、未固定 suite 输入或可能被定时事件误触发的其他 Job。

### Compose 公开地址专项

`compose-public-origin` 还在同一隔离 Runner 上以 `addp-runtimes` 启动 GeoPython、以 `business` 启动 MinIO；断言两个真实容器的 project 标签、网络归属、健康状态和宿主机端口边界，并验证 Runtime 能访问 System、Business MinIO 的宿主机入口使用选定回环端口。退出时分别清理 `addp-runtimes`、`business`、`addp-platform` 和 `addp-infra`，核对容器、网络、卷零残留。

### macOS 专用环境字段

#### 公共配置与凭据文件

常规 macOS self-hosted Runner 管理员应以根 `.env.example` 为字段清单，在仓库外创建独立环境文件并替换全部 Secret；不能把该文件复制为仓库根 `.env`。除专用 Infra 连接和所有内置 `*_SERVICE_CLIENT_SECRET` 外，至少显式配置 `ADDP_ONLINE_TEST=1`、`ADDP_ONLINE_TEST_TENANT_ID`、`POSTGRES_DB=addp_online`、`SYSTEM_URL`、`GATEWAY_URL`、`MANAGER_URL`、`META_URL`、`SECURITY_URL`、`TRANSFER_URL`、`CATALOG_URL`、`ASSET_URL`、`PORTAL_URL`、`DEVELOP_URL`、`SERVICE_URL`、`WORKBENCH_URL`、`CONSOLE_URL`、`STANDARD_URL`、`MODEL_URL`。

GitHub Environment `addp-online` 的 Repository Variable `ADDP_ONLINE_ENV_FILE` 只保存该仓库外文件的绝对路径，不保存文件内容。完整必需字段以[Host Gate 的 `REQUIRED_SUITE_ENV`](../test/online-host-gate.sh)为准；下表保留场景补充准备与身份区别，业务断言和清理职责由对应专题维护，不作为全部 suite 的配置清单。

#### 按场景补充

| 场景正文 | 专用准备与身份 |
| --- | --- |
| [模块注册恢复](Online引擎与消费.md#模块注册恢复) | `module-registry-recovery` 使用其中的 `MANAGER_SERVICE_CLIENT_SECRET`。 |
| [标准与模型引用删除](Online目录与治理.md#标准与模型引用删除) | `standard-model-reference-deletion` 使用 `ADDP_ONLINE_TEST_USER_ACCESS_TOKEN`。 |
| [消费方引擎恢复](Online引擎与消费.md#消费方引擎恢复) | `consumer-engine-recovery` 另外要求同一专用 User 的 `ADDP_ONLINE_TEST_USER_USERNAME`、`ADDP_ONLINE_TEST_USER_PASSWORD`，以及稳定 `ADDP_ONLINE_TEST_ENGINE_ID`、名称、端口、用户、密码和数据库。 |
| [企业目录发布](Online目录与治理.md#企业目录发布) | `enterprise-catalog-publishing` 同样使用该专用 User 的用户名和密码，并需预置永久 Domain 和 Department ID。 |
| [Workbench 服务消费](Online引擎与消费.md#workbench-服务消费) | `workbench-service-consumption` 另需同 Tenant 专用 `ADDP_ONLINE_TEST_TENANT_ADMIN_ACCESS_TOKEN`、永久 `ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID`，以及端口、数据库、只读用户、只读密码和 root fixture 密码，其浏览器阶段仍使用非管理员专用 User 的用户名和密码。 |
| [Transfer 仅新增写入](Online引擎与消费.md#transfer-仅新增写入) | `transfer-insert-only-mysql` 复用永久 PostgreSQL Engine Instance，另需永久 `ADDP_ONLINE_TRANSFER_MYSQL_ENGINE_ID`、名称、端口、数据库、可写用户、可写密码和仅供 Fixture 使用的 root 密码，浏览器使用同一专用 User 的用户名和密码。 |
| [OceanBase 消费](Online引擎与消费.md#oceanbase-消费) | `oceanbase-consumer-flow` 另需永久 `ADDP_ONLINE_OCEANBASE_ENGINE_ID`，以及该实例对应的端口、database、租户限定用户和密码，专用 User 只需具备 Meta、Manager、Transfer、Develop 和 Service 所需最小权限，其中包括 `manager.data_item.read`；Engine 控制面状态由部署 profile 负责验证，业务 consumer 不需要 `system.engine.read`。 |
| [Transfer 保护](Online数据保护.md#transfer-固定夹具) | `security-transfer-protection` 另需永久 `ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID`、MongoDB 端口、数据库、只读账号，以及仅供 Fixture 使用的 root 账号。 |

## 模块实例运行日志验收

Infra 日志和指标的启动选择、失败隔离及容器归属回归使用 `make test-infra-runtime-log-lifecycle`。它由 `make test-dev-lifecycle` 唯一调用，并随 `make test-platform` 进入现有 Platform CI；测试使用临时工作区和 Shell Docker 模拟，不启动个人 Infra。完整 `up.sh` 场景的子进程上限为 30 秒，模拟命令按参数个数和 NUL 分隔记录，覆盖空参数、回车、换行及中文保真。

模块实例运行日志 T2 入口为 `make test-system-runtime-log`，由 System owner 自建 disposable MinIO/Alloy/Loki 并清理全部测试资源；构建共享启动工具、身份关联、授权代理和重建保留同次验证，具体范围见[Infra 指南](../infra/README.md)。

## 平台节点指标 Hosted 验收

### 场景与身份

`platform-node-metrics` 使用 `scripts/test/online-hosted-node-metrics-gate.sh` 在干净 Hosted Linux x86_64 部署验证真实 System / Gateway / Monitor / Prometheus / node_exporter 链路。正式三员 Bootstrap 只准备临时登录凭据，平台 User 经真实 MFA 取 Token；两个非默认 Tenant 的最小权限 User 均应被平台入口拒绝，Prometheus 仅持有独立发现权限。

采集中心由标准 Infra 启动，节点由 `scripts/infra/node-metrics.py` 独立部署。临时 TLS 转发只连接真实 Gateway；监听地址取原生 Docker bridge Gateway 并限定端口，不对外放开来源准入。suite 核验保存版本、发现版本、活动目标及新鲜样本，验证来源/控制面中断恢复，以及节点和目标停用后的撤除。报告和 Hosted 清理摘要分别记录业务断言与部署零残留。

Monitor 使用独立查询客户端证书，私钥不挂载到中心；在标准 Infra 启动后核实中心所有权和实际 loopback 映射，再提供查询 origin 并启动 Monitor。

当前身份、case 和实际可执行范围见[平台观测正式场景](../../docs/spec/testing/scenarios/平台观测与语义生命周期.md#platform-node-metrics)，资源查询契约与 Hosted 操作边界分别见[资源查询与预算](../../docs/spec/addp平台运行观测规范.md#62-资源查询与预算)和[Hosted 身份与操作范围](../../docs/spec/addp平台运行观测规范.md#92-hosted-身份与操作范围)。下列旧版结果按各自范围保留，不充当当前完整能力清单。

### 验证入口

确定性门禁为 `make test-node-metrics-online-runner`，由 `make test-online-runner`、`make test-platform` 和既有 Platform CI 聚合；身份数据库验证进入 `bash scripts/test/system-iam-postgres-gate.sh --package online-fixture`。真实 T4 使用 `make test-online ONLINE_SUITE=platform-node-metrics`，在 `Online T4 gates` 手工选择同名 suite，首次真实通过前不加入 schedule。本地 macOS 不启动这套部署，也不以 T2 协议夹具替代 T4。

### 节点指标固定版本证据

节点系列的固定版本、失败和未覆盖项见[Monitor 综合历史证据](../../docs/spec/testing/evidence/Monitor综合验收历史证据.md#节点实例与资源告警)。下列两次运行仍按各自原始范围认定。

节点指标 suite 的首次真实 Hosted T4 已在 `456d3bb84` 通过：[Run 37463003308](https://github.com/pampa0629/addp/actions/runs/37463003308)。业务报告的阶段为 `complete`，保存与活动目标版本均为 2；独立清理摘要确认部署/凭据销毁及 Infra 零残留。原生 OAuth `token_type` 按既有 Common Client 的大小写不敏感标准校验，保持唯一认证协议。该固定版本结果仅覆盖当次 Linux VM 链路，不证明生产 T5 或用户资源查询页面交付；suite 继续手工触发。

历史查询扩展已在 `794450a30` 通过 [Run 37485747576](https://github.com/pampa0629/addp/actions/runs/37485747576)：覆盖八项基础节点资源的正式即时/趋势 API、查询预算 CAS/热生效、查询权限与身份隔离、中心故障时 Ready/租户执行列表继续可用、新样本恢复、节点重启用和停用后不复用历史。独立摘要确认清理成功与 Infra 零残留；18 个归档文件的私钥及完整 IAM 不透明凭据检查通过。该扩展与首次通过的原节点／目标采集链路证据分别认定，均不代表当前 HEAD 已重跑。
