# ADDP 测试与验收规范

本文定义 ADDP 测试分层、标准入口、环境边界、CI 编排和 Online / Release 验收的稳定规则。具体可用目标、suite 登记和运行参数以根目录 `Makefile`、`scripts/README.md`、`scripts/test/` 及模块 owner 文档为准。

阅读导航：

| 阅读目的 | 文档入口 |
| --- | --- |
| 了解统一规则与执行入口 | [基本原则](#一基本原则)、[测试分层](#二测试分层)、[标准入口](#三标准入口) |
| 确认确定性测试及基础设施边界 | [T0–T3 测试规范](testing/T0-T3测试规范.md) |
| 执行跨模块 Online 验收 | [T4 Online 验收规范](testing/T4-Online验收规范.md)、[场景索引](#online-场景索引) |
| 确认产品或 Runtime 的发布条件 | [T5 发布认证规范](testing/T5-发布认证规范.md) |
| 核对登记与交付要求 | [CI 与交付规范](testing/CI与交付规范.md) |
| 查阅特定 owner 的补充要求 | [Owner 补充门禁](testing/Owner补充门禁.md) |
| 复核历史证据及适用范围 | [验收证据索引](testing/验收证据索引.md) |

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

# 仅运行 owner 的 Go T1，便于开发定位；不计为完整模块门禁
make test-module MODULE=<module> GO_ONLY=1

# 全部已登记 disposable 基础设施门禁
make test-integration

# 仅在持有合法 License 的 owner 受控 Linux 主机人工运行，不进入 GitHub Actions
make test-integration-owner-managed

# 本地与 CI 共用的发布工作流安全审计（首次执行需下载固定版本工具）
make test-workflow-security

# 一个已登记的跨模块 Online suite
make test-online ONLINE_SUITE=<suite>

# 一个已登记的产品发布 suite
make test-release RELEASE_SUITE=<suite>
```

约束如下：

- `make test-changed` 与 CI 共用 owner 影响计算；共享代码改动按真实依赖扩散到已登记消费者。
- `make test-module` 自动发现模块的 Go、Python、CMake/C++、前端与基础设施门禁，不维护第二份模块清单。
- `make test-integration` 严格串行调用 owner 的 T2 事实入口，不复制测试逻辑。
- `test-online` 与 `test-release` 必须显式选择已实现的 suite；未实现能力不得以占位 suite 登记。
- 不提供跨 T0-T5 的 `test-all`。T4 与 T5 的身份、基础设施和安全前置条件不同，完整认证由 CI 分别编排标准入口并分别报告。
- 开发者和 AI 不得自行拼接一组语言命令替代上述标准入口；模块内部命令仅用于 owner 开发与标准入口实现。

## Online 场景索引

先按[T4 Online 验收规范](testing/T4-Online验收规范.md)确认运行条件，再进入对应场景。专项条款的适用范围以声明的 suite 与 profile 为准。
| 场景类别 | 文内入口 |
| --- | --- |
| 授权与共享 | [PostgreSQL 源数据授权](testing/scenarios/授权与共享.md#pg-source-authorization)、[企业目录发布](testing/scenarios/授权与共享.md#enterprise-catalog-publishing)、[Workbench 服务消费](testing/scenarios/授权与共享.md#workbench-service-consumption) |
| 数据保护 | [四出口数据保护](testing/scenarios/数据保护.md#security-mysql-owner-protection)、[限时原值访问](testing/scenarios/数据保护.md#security-plaintext-access)、[Transfer 保护出口](testing/scenarios/数据保护.md#security-transfer-protection) |
| 部署与执行恢复 | [模块注册与恢复](testing/scenarios/部署与执行恢复.md#module-registry-recovery)、[消费方与引擎动态恢复](testing/scenarios/部署与执行恢复.md#consumer-engine-recovery)、[Compose 生产单入口](testing/scenarios/部署与执行恢复.md#compose-public-origin)、[Orchestrator 执行](testing/scenarios/部署与执行恢复.md#orchestrator-execution)、[质量动态绑定](testing/scenarios/部署与执行恢复.md#quality-dynamic-binding) |
| 平台观测与语义生命周期 | [平台节点指标](testing/scenarios/平台观测与语义生命周期.md#platform-node-metrics)、[本体修订生命周期](testing/scenarios/平台观测与语义生命周期.md#ontology-revision-lifecycle)、[指标服务修订生命周期](testing/scenarios/平台观测与语义生命周期.md#metric-service-revision-lifecycle) |
| Manager 与空间成果 | [Manager 内部产物](testing/scenarios/Manager与空间成果.md#manager-internal-artifact-lineage)、[Manager 混合检索](testing/scenarios/Manager与空间成果.md#manager-hybrid-search)、[矢量 PMTiles](testing/scenarios/Manager与空间成果.md#vector-pmtiles-workflow)、[栅格工作流](testing/scenarios/Manager与空间成果.md#raster-workflow)、[等值线持久化](testing/scenarios/Manager与空间成果.md#raster-contour-persistence)、[分类转面持久化](testing/scenarios/Manager与空间成果.md#raster-polygonize-persistence)、[矢量栅格化持久化](testing/scenarios/Manager与空间成果.md#vector-rasterize-persistence) |
| 同步与引擎消费 | [关系型引擎消费](testing/scenarios/同步与引擎消费.md#关系型引擎消费链路)、[MySQL 仅新增同步](testing/scenarios/同步与引擎消费.md#transfer-仅新增-mysql-同步)、[关系型 SQL ETL 与字段血缘](testing/scenarios/同步与引擎消费.md#transfer-relational-sql-etl)、[Transfer continuous 过程事件](testing/scenarios/同步与引擎消费.md#transfer-continuous-events) |


瀚高与 HDFS/Spark 的专用条款同属[同步与引擎消费](testing/scenarios/同步与引擎消费.md)。
