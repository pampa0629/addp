# ADDP 脚本：Online引擎与消费

[返回脚本导航](../README.md) · [返回测试与发布入口](测试与发布入口.md#test---测试与发布门禁)

本页导航：[模块与消费方恢复](#模块与消费方恢复) · [关系型 SQL 与指标服务](#关系型-sql-与指标服务) · [Workbench 与关系引擎](#workbench-与关系引擎) · [Redis 消费](#redis-消费)

测试分层、身份和清理规则以[测试与验收规范](../../docs/spec/addp测试与验收规范.md)为准；历史结果仅适用于所列源码和运行尝试，不代表当前 HEAD 已重跑。

公共准备统一查阅[部署 profile 与触发](Online编排与观测.md#部署-profile-与触发)、[macOS 环境字段](Online编排与观测.md#macos-专用环境字段)、[身份与拓扑预检](../../docs/spec/testing/T4-Online验收规范.md#开关身份与拓扑预检)、[报告脱敏](../../docs/spec/testing/T4-Online验收规范.md#报告)及[数据、超时与清理](../../docs/spec/testing/T4-Online验收规范.md#数据超时与清理)。各场景的专用身份、物理夹具、业务断言和固定版本结果见下文，准备与清理不混用不同 profile 的假设。

场景直达：[模块注册恢复](#模块注册恢复) · [消费方引擎恢复](#消费方引擎恢复) · [关系型 SQL ETL](#关系型-sql-etl) · [指标修订生命周期](#指标修订生命周期) · [Workbench 服务消费](#workbench-服务消费) · [OceanBase 消费](#oceanbase-消费) · [openGauss 消费](#opengauss-消费) · [TiDB 消费](#tidb-消费) · [Transfer 仅新增写入](#transfer-仅新增写入)

## 模块与消费方恢复

### 模块注册恢复

`module-registry-recovery` 的环境字段沿[macOS 场景准备](Online编排与观测.md#按场景补充)，完整必需项以[Host Gate](../test/online-host-gate.sh)为准；模块服务凭据使用既有 `MANAGER_SERVICE_CLIENT_SECRET`，正式进程与探针的分工见[注册恢复场景](../../docs/spec/testing/scenarios/部署与执行恢复.md#module-registry-recovery)。

Host Gate 先通过受保护的正式进程入口依次启动 Manager、System、Gateway，验证 Manager 在 System 前为 Alive/Not Ready 且业务路由返回 `module_not_ready`，System 到达后以同一 `instance_id` Ready，Gateway 首个快照建立路由；随后停止 System，等待 Manager 转为 Not Ready、租约路由消失，再恢复 System 并验证 Manager 和 Gateway 无需重启即恢复。五个阶段分别写入 `module-lifecycle-*.json`。

正式 Manager 优雅停止后，owner 套件再使用 `addp-manager` Platform Service Principal 获取短期 Token，经 System 正式注册、心跳和注销 API验证两个回环探针的幂等注册、租约失效、同 ID 恢复、双实例路由、发布元数据更新、优雅注销和零活跃实例残留。`ADDP_ONLINE_PROCESS_TIMEOUT_SECONDS`、`ADDP_ONLINE_ROUTE_TIMEOUT_SECONDS` 和 `ADDP_ONLINE_LEASE_TIMEOUT_SECONDS` 分别控制正式进程、路由和租约收敛上限。

### 消费方引擎恢复

`consumer-engine-recovery` 采用 macOS 专用全量部署，准备字段沿[场景环境表](Online编排与观测.md#按场景补充)，首次导航、永久引擎与物理端点控制沿[恢复场景](../../docs/spec/testing/scenarios/部署与执行恢复.md#consumer-engine-recovery)。Host Gate 通过 `business/scripts/online-engine-fixture.sh` 管理物理端点，该入口只允许 `ADDP_ONLINE_HOST=1` 的 macOS 专用 Runner，使用独立 `ADDP_ONLINE_TEST_ENGINE_*` 变量并拒绝接管非 `business/postgres` Compose 容器；它不读取或创建 `business/.env`。suite 使用真实测试 User 用户名和密码登录 Console，并以同一 User 的 Access Token 校验 Tenant AuthContext、`system.engine.read`、`system.engine.execute` 和被测页面读取权限。

Configuration、Manager Data Explorer、Service Query Services 在同一 Browser Context 中各首次导航一次并等待自身首个请求成功；随后保持同一 Manager iframe，停止/恢复物理 Fixture，并通过 `POST /api/v1/system/engines/{id}/test` 记录 `offline → online`，页面必须通过既有轮询自动收敛。Manager、Service Backend 与 Console、Manager、Service Frontend 的 PID 前后必须一致。Engine Instance 由专用环境长期预置，suite 不创建、删除或修改其身份；退出路径恢复 `online` 后再停止 Fixture 与应用。

## 关系型 SQL 与指标服务

### 关系型 SQL ETL

`transfer-relational-sql-etl` 的唯一 Hosted 部署与完整覆盖范围见[正式场景](../../docs/spec/testing/scenarios/同步与引擎消费.md#transfer-relational-sql-etl)，在 `Online T4 gates` 手工选择同名 suite。后续扩展分别见[Manager 私有导入与对象上传](../../docs/spec/testing/scenarios/同步与引擎消费.md#manager-私有导入)、[写入失败与证据边界](../../docs/spec/testing/scenarios/同步与引擎消费.md#写入失败与证据边界)、[字段级血缘](../../docs/spec/testing/scenarios/同步与引擎消费.md#字段级血缘验收)和[扫描读取隔离](../../docs/spec/testing/scenarios/同步与引擎消费.md#扫描调用链与读取隔离)；下列原生表和查询源子场景不作为完整 suite 的覆盖清单。

准备由[Transfer Host Gate](../test/online-hosted-transfer-gate.sh)复用 Hosted 生命周期，当前参与服务及物理夹具以该入口为准。真实测试 User 的权限集由[System 身份夹具](../../system/backend/cmd/online-test-fixture/main.go)维护，独立 Provisioner 通过 System API 注册当次 Engine Instance，注册后清除 Provisioner Token；不以固定权限数量描述后续扩展。Business Fixture 使用本轮独占的 tmpfs 容器，数据库用户只读取源表并在指定 schema 创建自己的目标表。业务断言仍只有 `make test-online ONLINE_SUITE=transfer-relational-sql-etl` 一条入口。

同一 suite 验证原生表字段改名、decimal 精度转换、常量生成、replace 旧入边关闭和两跳自动采集，并从 Console Manager iframe 验证字段图与结构快照。

SQL 查询源由真实浏览器构造投影和参数化过滤，验证 2 行结果及字段血缘 unavailable。

临时任务及后续扩展的编排定义按当次捕获 ID 经正式 API 清理并确认 404，Business owner 独立核对目标数据、精度及各领域物理事实；不以旧任务或目标表数量判断完整清理。

成功、失败和中断路径均销毁业务容器、Infra 数据卷及 owner-only 凭据目录，残留使门禁失败；强制终止不能声称清理完成。Actions Artifact 仅归档报告、截图、脱敏日志和清理证据，领域报告与完整 suite 的结论按上述专题分别认定。确定性验证使用 `make test-online-runner` 与 `make test-platform`；首次真实 T4 成功前只登记手工触发。

### 指标修订生命周期

`metric-service-revision-lifecycle` 的 Hosted 准备、夹具及两引擎范围见[正式场景](../../docs/spec/testing/scenarios/平台观测与语义生命周期.md#metric-service-revision-lifecycle)，生命周期使用[Metric Host Gate](../test/online-hosted-metric-gate.sh)。在 `Online T4 gates` 手工选择同名 suite，以 `metric_engine` 选择 `postgresql` 或 `tidb`；手工只验收所选引擎，每日夜间在独立 Job 中分别验收两个引擎。

所选业务引擎为只读，TiDB 使用固定 digest 的 PD/TiKV/TiDB 临时 Compose 集群；两种引擎均通过真实 API 扫描并建立 Standard 定义、逻辑表、关系和指标修订，复用同一指标契约和确定性结果。当次身份凭据与引擎描述仅写入 `runner.temp` 的 owner-only 文件，不接受永久业务环境提供的 ID 或数据。

业务断言仍只走 `make test-online ONLINE_SUITE=metric-service-revision-lifecycle`：首次查询得到非空确定结果；发布新修订不自动重绑；旧修订撤回后 Model 返回 `409 metric_implementation_state_conflict`、Service 返回 `500 query_execution_failed` 且服务仍 active、绑定不变；显式重绑后恢复相同数据。临时服务按 ID 删除并确认 404；指标历史在运行期间保留，随后与整套临时部署共同销毁，无历史记录强删旁路。

报告 `metric-service-revision-lifecycle.json` 记录引擎类型、Run ID、实现/修订/服务 ID、校验结果与临时服务清理状态；Hosted `summary.txt` 另记录部署销毁结果。两者及脱敏日志由 Actions Artifact 保留 14 天，不归档凭据、连接密码、原始计划或查询结果正文。强制终止时不得声称清理通过，Runner 的临时 VM 回收承担最终隔离。确定性回归统一由 `make test-online-runner` 与 Platform CI 覆盖；PostgreSQL 与 TiDB 已在同一提交分别完成首次真实通过，夜间两个 Job 保留独立引擎标识和清理证据。

## Workbench 与关系引擎

### Workbench 服务消费

`workbench-service-consumption` 的 macOS 部署和环境字段沿[场景准备](Online编排与观测.md#按场景补充)，双身份与当前断言见[正式场景](../../docs/spec/testing/scenarios/授权与共享.md#workbench-service-consumption)。Host Gate 启动 System、Gateway、Service、Workbench 和 Console，由 `business/scripts/online-workbench-mysql-fixture.sh` 管理独立 Business MySQL；Fixture 只接受仓库外 `ADDP_ONLINE_WORKBENCH_MYSQL_*`，不读取或创建 `business/.env`；它重建仓库已有确定性样例并把 Engine 使用的账号收敛为仅有 `SELECT` 的读取账号。

suite 分离非管理员专用 User Token 与同 Tenant 的专用 Tenant Administrator Token：前者使用永久 MySQL Engine Instance，经 Gateway 检测 Service 输出契约，临时发布主 SQL 服务 `commerce-order-analysis` 和同契约未授权服务 `commerce-order-analysis-denied`，再经 Consumer Descriptor 直接创建含 Table、Chart 两个 Component 的未发布 Data Application；后者只经 System 正式 API 创建仅授权主服务的 API 消费方和一枚进程内凭据。

API 层真实验证 cursor、动态筛选、标量类型、有限 CSV、无空间输出、契约指纹变化，以及 API Key 对已授权服务的 200、对未授权服务的 403、对控制面的 401 和删除后 45 秒内收敛失效；浏览器层以非管理员 User 登录 Console，在正式 Component 编辑器中验证两种 renderer 及契约变化后的预览阻断。

Host Gate 安装专用 Chromium；完整 API Key 不得进入报告或日志，API 消费方及级联凭据、未发布 Data Application 与两个 Query Service 只按本轮创建 ID 在 `finally` 删除，不使用名称前缀或数据库清理，也不为自动化测试增加已发布应用强删旁路。

### OceanBase 消费

`oceanbase-consumer-flow` 的 macOS 环境字段沿[场景准备](Online编排与观测.md#按场景补充)，引擎模式、稳定键和消费断言沿[OceanBase 正式场景](../../docs/spec/testing/scenarios/同步与引擎消费.md#oceanbase)。参与 System、Gateway、Meta、Manager、Transfer、Develop 和 Service；`business/scripts/online-oceanbase-consumer-fixture.sh` 管理独立 OceanBase CE。

专用环境预置永久 `engine_type=oceanbase` Engine Instance；Fixture 固定维护 5 行 InnoDB watermark 源表和同构空目标表。

suite 经 Gateway 和 Meta 获取正式 ResourceLocator，只创建一条 bounded watermark + upsert Transfer 任务，连续执行并精确断言 5 行首批、2 行增量和 0 行空增量；Manager 表预览、Develop SQL 与临时 Query Service 通过各自正式数据出口读取同一目标表，最终必须得到相同的 6 行 checksum，Manager 还校验扫描列顺序。

任务和服务按捕获 ID 删除并确认 404，Fixture 在退出路径恢复源表基线和空目标表。该 suite 仅可从手工 workflow 运行，首次真实通过前不得增加 schedule。

### openGauss 消费

`opengauss-consumer-flow` 使用同一个 `relational-consumer-flow-online.py` 断言 Manager、Transfer、Develop 和 Service，上层流程只通过通用 Engine Plugin/Provider 和 SQLDialect 工作。

openGauss 6.0.6 官方容器的 NUMA 限制要求使用唯一 GitHub Hosted `ubuntu-24.04` x86_64 profile，见[openGauss 正式场景](../../docs/spec/testing/scenarios/同步与引擎消费.md#opengauss)。[openGauss Host Gate](../test/online-hosted-opengauss-gate.sh)按正式 Infra 生命周期在镜像缺失时从 `scripts/infra/Dockerfile.postgres` 构建 x86_64 PostgreSQL 镜像，再从固定 SHA-256 官方介质创建独占 openGauss，准备当次 Infra 和 Manager/Transfer/Develop/Service 参与链路。

Business Fixture 只输出 owner-only Engine 描述，System IAM owner helper 创建非默认 Tenant、仅含租户可定制业务 Permission 的最小消费 User，并把一次性 Engine Provisioner 绑定到正式内置 `tenant.infrastructure_administrator` Role。通用 `online-engine-registration.py` 使用 Provisioner Token 通过正式 System API 注册并验证当次 Engine Instance，随后立即从业务断言环境清除该 Token 与数据库凭据；consumer 不访问 System Engine 控制面，只验证 Manager、Transfer、Develop 和 Service 的消费路径，证据另写入 artifact 目录。

成功、失败和中断均须停止应用、删除本轮 openGauss 容器并销毁 Infra 数据卷；一次性环境与凭据准入沿[T4 专用环境](../../docs/spec/testing/T4-Online验收规范.md#专用环境)，不复用 Business `.env` 或外部实例。

2026-09-10 的 [Online T4 run 34469670386](https://github.com/pampa0629/addp/actions/runs/34469670386) 已确认构建身份、5/2/0 watermark、三出口同 checksum、清理和零残留全部通过，因此该 suite 同时保留手工 `workflow_dispatch`，并在每日 `20:30 UTC` 仅调度其 Hosted profile；不把该固定版本结果当作当前 HEAD 已重跑。

### TiDB 消费

`tidb-consumer-flow` 与 OceanBase/openGauss 复用同一个 `relational-consumer-flow-online.py`，使用 `engine_type=tidb` 的 database catalog profile，不在 Manager、Transfer、Develop 或 Service 增加数据库分支。

macOS 专用 Runner 预置永久 TiDB Engine Instance；[TiDB 正式场景](../../docs/spec/testing/scenarios/同步与引擎消费.md#tidb)维护部署与三组件清理边界。仓库外环境文件提供 `ADDP_ONLINE_TIDB_ENGINE_ID`、`ADDP_ONLINE_TIDB_PORT`、`ADDP_ONLINE_TIDB_DATABASE`、`ADDP_ONLINE_TIDB_USER` 和可为空的 `ADDP_ONLINE_TIDB_PASSWORD`，完整必需项沿[Host Gate](../test/online-host-gate.sh)。

`online-tidb-consumer-fixture.sh` 复用 T2 的固定 digest、无卷 PD/TiKV/TiDB Compose，重建 5 行源和空目标，推进 2 行 watermark 后由同一 Transfer 任务得到 5/2/0 读取计数，并由 Manager、Develop、Service 比较 6 行同 checksum。

任务、服务和三组件在退出路径清理并验证零残留。首次真实通过前仅允许手工 `workflow_dispatch`，不得加入 schedule。

### Transfer 仅新增写入

`transfer-insert-only-mysql` 的 macOS 字段与两端身份沿[场景准备](Online编排与观测.md#按场景补充)，单字段水位边界沿[正式场景](../../docs/spec/testing/scenarios/同步与引擎消费.md#transfer-仅新增-mysql-同步)。参与 System、Gateway、Meta、Transfer 和 Console；`business/scripts/online-transfer-insert-only-fixture.sh` 同时管理专用 PostgreSQL 源表和可写 MySQL 目标库。

suite 以真实 User 登录 Console，经 Transfer 向导选择两端资源；字段映射阶段必须调用正式推荐接口，把无声明精度的 `numeric` 当前值分析为 `DECIMAL(6,2)`，配置阶段再选择仅新增并自动派生 `id` 水位和目标键。

页面创建后首轮必须写入 6 行；Fixture 修改旧 `id=1` 并新增 `id=7` 后，从任务详情再次执行只能写入 1 行，最终目标共 7 行、旧值不变、新值存在且无重复键。

任务按捕获 ID 删除并确认 404，复合 Fixture 在所有退出路径删除固定表并停止两个容器。该 suite 仅可从手工 workflow 运行，首次真实通过前不得增加 schedule。

## Redis 消费

Redis Online 验收使用 `make test-online ONLINE_SUITE=redis-consumer-flow`，在 `online-t4-gates.yml` 手工选择同名 suite，由 `redis-hosted-t4` Job 执行 `bash scripts/test/online-hosted-redis-gate.sh`。该入口复用[Hosted 生命周期](../utils/hosted-online.sh)，准入、部署隔离与报告规则沿[Online 编排](Online编排与观测.md#部署-profile-与触发)。

Business owner 脚本 `business/scripts/online-redis-consumer-fixture.sh` 提供固定镜像、只读 ACL 与九个原生样例，System owner helper 创建普通验收用户及独立 Engine provisioner，登记后移除 provisioner 凭据。

验收覆盖 System 实时目录、Meta 扫描、Manager 原生预览以及真实 Console Meta 重扫和 Manager 页面，截图、API 与同一 User/Tenant 的浏览器报告归档到仓库外 artifact 目录。

脚本/断言确定性门禁是 `make test-online-runner`；登记检查从 Hosted metadata 自动发现 suite 并核对 workflow choice，不需要额外 owner 清单。该 gate 只接受 Hosted，拒绝 macOS、脏 checkout、个人环境与已有资源；当次资源清理与零残留按[T4 清理规则](../../docs/spec/testing/T4-Online验收规范.md#数据超时与清理)核对。首次真实运行前不计为 T4 通过，不纳入夜间执行。
