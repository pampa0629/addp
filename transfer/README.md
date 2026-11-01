# Transfer 数据传输模块

Transfer 是 ADDP 的数据传输中枢，负责传输任务配置、执行编排、字段映射、异步 worker、checkpoint 观测、执行日志、指标和写后 Meta 扫描触发。

当前已实现 bounded snapshot、bounded watermark incremental、业务 Kafka keyed JSON continuous upsert + DLQ、业务 Kafka bounded replay，以及 PostgreSQL/MySQL/Oracle 单表 CDC initial snapshot + upsert/delete。引擎来源、目标与非空间限制统一见[当前支持矩阵](./docs/transfer-任务语义与同步模式.md#三当前支持矩阵)；continuous worker、Console Wizard 和诊断能力见[Continuous/Kafka 配置](./docs/transfer-基本概念及配置说明.md#八continuouskafka-v1-契约与当前实现边界)。

Oracle 空间捕获与目标类型限制由[CDC 配置](./docs/transfer-基本概念及配置说明.md#九数据库-cdc-v1-已冻结边界)维护；未支持项与待评估增强统一见[后续能力清单](../docs/next/transfer后续能力清单.md)。能力摘要不作为当前版本运行通过记录，证据与未覆盖项见[验证范围](./docs/transfer-任务语义与同步模式.md#十一验证范围)，标准门禁及 Oracle 专项缺口见本页[启动与验证](#启动与验证)。

当前主路径采用 clean break：Transfer 不再维护私有 reader / writer 插件体系，不再兼容旧 `connector_type`、`source_config`、`target_config`、`output_format`、`file_type` 等任务 JSON 字段。具体读写能力来自 `common/engine`、`common/format`、`common/contentio` 和 `common/engine/contentadapter`。

## 模块边界

| 层 | 职责 |
|---|---|
| Transfer | 任务 JSON、planner、policy、transform、worker、checkpoint、logs、metrics、写后 Meta scan |
| `common/engine` | 引擎连接、catalog、metadata、content read/write、table batch/session read/write、change stream read、DeleteResource |
| `common/format` | table reader / writer、multi table reader / writer、scope table reader、格式编码解码 |
| `common/contentio` | content `Ref`、Reader、Writer、Lister、RangeReader |
| `common/engine/contentadapter` | engine content provider 到 contentio 的适配 |

## 已稳定的 table Transfer 主链路

table 类型 Transfer 已形成统一主路径：

```text
source endpoint
  -> table reader
  -> table batch
  -> field_mapping transform
  -> table writer
  -> target endpoint
```

endpoint 只决定 reader / writer 来源：

| endpoint | 读写来源 |
|---|---|
| native table | `common/engine` table read / write session 或 batch provider |
| encoded single file/object | engine content provider + contentio + `common/format` table reader / writer |
| encoded multi file/object | contentio + `[]format.RelatedRef` + `common/format` multi table reader / writer |
| encoded whole scope | contentio reader/lister + `common/format` scope table reader |

table 格式与原生目标写侧统一见[格式支持](./docs/transfer-基本概念及配置说明.md#33-格式)和[原生目标写侧](./docs/transfer-基本概念及配置说明.md#已接入的原生目标写侧)；Parquet 默认压缩及动态 codec 声明见[Parquet 列式压缩](./docs/transfer-基本概念及配置说明.md#parquet-列式压缩)。

non-table raw copy 的 endpoint 与格式继承见[raw copy 支持范围](./docs/transfer-基本概念及配置说明.md#四raw-copy-支持范围)；当前限定 `document`、`media`、`cad`、`unknown` 的 encoded single file/object，按原始字节复制，目标仅支持 `replace`。它不进入 `common/format` table reader / writer，不解析正文、不抽取媒体元数据，也不做格式转换。

模块与 Provider 分工以[任务语义](./docs/transfer-任务语义与同步模式.md#一模块定位)为准。本节主链路、支持声明或单项读写结果不代表完整运行链路已验收；标准入口见[启动与验证](#启动与验证)，固定版本证据、失败及未覆盖项见[验证范围](./docs/transfer-任务语义与同步模式.md#十一验证范围)。

## 任务配置

任务配置存放在 `transfer.transfer_tasks.config` 中，必须使用 source / target endpoint 结构：

```json
{
  "runtime": {"boundary": "bounded"},
  "load": {"mode": "snapshot"},
  "source": {
    "locator": "addp://engine/1/path/public/roads?type=table",
    "data_type": "table",
    "representation": "native"
  },
  "target": {
    "parent_locator": "addp://engine/2/path/exports?type=directory",
    "name": "roads.parquet",
    "data_type": "table",
    "representation": "encoded",
    "format": "parquet",
    "policy": {"apply_mode": "replace"}
  },
  "transforms": [
    {
      "type": "field_mapping",
      "version": "v1",
      "mode": "project",
      "fields": [
        {"source": "name", "target": "road_name", "target_type": "string"},
        {"source": "geom", "target": "geometry", "target_type": "geometry"}
      ]
    }
  ],
  "batch_size": 10000
}
```

写入策略和支持组合统一见[写入策略](./docs/transfer-基本概念及配置说明.md#六写入策略)及[当前支持矩阵](./docs/transfer-任务语义与同步模式.md#三当前支持矩阵)：snapshot 使用 `replace|append`，watermark 和业务 Kafka 使用幂等 `upsert`，数据库 CDC 使用 `upsert_delete`；目标按 Engine Provider/capability 选择，旧 `write_mode` 不兼容。

[Continuous/Kafka 配置](./docs/transfer-基本概念及配置说明.md#八continuouskafka-v1-契约与当前实现边界)维护业务 Kafka keyed JSON upsert、显式 `block|dead_letter` 和写入新 PostgreSQL 隔离表的 bounded replay；[CDC 配置](./docs/transfer-基本概念及配置说明.md#九数据库-cdc-v1-已冻结边界)维护 PostgreSQL/MySQL/Oracle 单表 initial snapshot + upsert/delete。两类路线复用同一个 continuous worker 与 `PartitionedTableChangeApplyProvider`；Oracle 空间捕获、目标 ledger 和类型限制由该配置正文维护，Infra Kafka 不进入 System engines 或用户任务 JSON。

CDC schema drift 使任务进入 `blocked` 且不推进当前 offset；PostgreSQL/MySQL 仅当前阻塞消息中的新增 nullable、非主键、非 geometry 字段可人工审批，审批后进入 paused，由用户显式 Resume。Oracle 任意 drift 和其他不可审批变化须 Stop 后新建任务与目标表，完整条件见[Schema drift 与审批恢复](./docs/transfer-基本概念及配置说明.md#schema-drift-与审批恢复)。

重跑和续读见[恢复分级](./docs/transfer-基本概念及配置说明.md#恢复分级)：snapshot replace 失败可从头 retry，append retry 被拒绝；watermark 与未阻塞的 continuous/CDC 从 `transfer.sync_states` committed position resume。retention 越界必须明确失败，不静默重置；目标锁等待须响应 context 取消并回滚未完成的业务行与 apply ledger，细则见[位置恢复与取消](./docs/transfer-基本概念及配置说明.md#位置恢复与取消)。

能力与配置说明不作为当前版本运行通过记录；提交顺序、DLQ/replay、来源隔离、恢复与回滚等须分别验收，标准入口与证据见[Continuous 验收边界](./docs/transfer-基本概念及配置说明.md#continuous-验收边界)及本页[启动与验证](#启动与验证)。各后续增强的独有验收限制见[清单验收范围与证据](../docs/next/transfer后续能力清单.md#六验收范围与证据)。

## API

路由前缀：`/api/v1/transfer`。

常用接口：

- `GET /ping`
- 资源选择、资源树和表字段读取统一走 Meta resource-tree / item API，Transfer 不提供私有数据源树代理。
- `GET /capabilities`
- `GET /task-provider/tasks`
- `GET /task-provider/tasks/:task_type/:id`
- `POST /task-provider/tasks/:task_type/:id/execute`
- `GET /task-provider/executions/:execution_id`
- `GET /task-definitions`
- `POST /task-definitions`
- `GET /task-definitions/statistics`
- `GET /task-definitions/:id`
- `PUT /task-definitions/:id`
- `DELETE /task-definitions/:id`
- `POST /task-definitions/:id/start`
- `POST /task-definitions/:id/pause`
- `POST /task-definitions/:id/resume`
- `POST /task-definitions/:id/stop`
- `POST /task-definitions/:id/replay`
- `GET /task-definitions/:id/schema-change`
- `POST /task-definitions/:id/schema-change/approve`
- `GET /task-definitions/:id/dead-letters`
- `GET /task-definitions/:id/dead-letters/:identity`
- `GET /task-definitions/:id/executions`
- `GET /executions`
- `GET /executions/:execution_id`
- `POST /executions/:execution_id/retry`
- `GET /executions/:execution_id/progress`
- `GET /executions/:execution_id/events`

DLQ 列表与详情使用 `transfer.task.read` 读取 Tenant/task 安全控制索引；来源隔离与 payload 安全见[公开查询专题](./docs/数据库架构.md#dlq-公开查询与来源隔离)，后台 availability 治理、瞬时失败与实际验收范围见[availability 专题](./docs/数据库架构.md#availability-失败与验收边界)。查询不探测 Infra Kafka，控制索引读取权不授予原始 payload 访问权。

`GET /task-provider/executions/:execution_id` 是只允许 Orchestrator Runtime 使用的 TaskProvider 标准执行状态入口；用户执行详情继续使用 `GET /executions/:execution_id`。两者都按统一 `common.task_executions.execution_id` 查询，但权限和调用主体不共用。重试、进度和日志入口也按 `execution_id` 定位执行记录。私有 task-definition `stop` 只控制 continuous runtime；bounded worker 仍不支持真实中断，因此 TaskProvider 保持 `supports_cancel=false`，不注册标准 execution cancel endpoint。

continuous worker 是独立进程角色 `cmd/continuous-worker`，用 Infra PostgreSQL 管理任务状态，通过 System Engine Resolver 连接业务 Engine；CDC 内部 Kafka 使用部署配置和独立 `transfer` principal，不注册为 System Engine。除 continuous runtime 配置外，CDC consumer 还使用 `INFRA_KAFKA_BOOTSTRAP_SERVERS`、`INFRA_KAFKA_TRANSFER_PASSWORD`、`INFRA_KAFKA_SECURITY_PROTOCOL` 和 TLS 配置。worker 要求 `SYSTEM_URL` 与 `TRANSFER_SERVICE_CLIENT_SECRET` 可用，并按任务 Tenant 获取短期 Service Access Token。

Orchestrator v1 的 TaskProvider 发现与执行使用独立 `/task-provider/` 入口，仅面向 bounded `sync` 任务；Console 用户任务列表使用 `/task-definitions`，可用 `runtime_boundary` 过滤。路径、身份、专用权限与 continuous 拒绝边界统一见[任务表 API](./docs/tables/tasks表.md#五api)。

## 启动与验证

### 开发启动与就绪检查

以下操作从仓库根目录由用户在自己的终端执行；基础设施准备、实际端口与 `/health/ready` 检查统一见[快速开始](docs/快速开始.md#一启动)，不从首选端口推断实际监听地址。

```bash
bash scripts/dev/start.sh -transfer
```

测试和构建不要求重启开发服务。确需加载后端改动时，由用户执行 `./scripts/dev/restart.sh -transfer`；生命周期锁、进程归属和停止失败边界沿[启动脚本指南](../scripts/docs/启动脚本使用指南.md#启动与重启边界)。就绪响应不证明 Worker 执行、源读、目标写或公开入口已验收。

### 常规模块与专项门禁

下列命令均从仓库根目录执行；测试分层和入口职责以[测试与验收规范](../docs/spec/addp测试与验收规范.md#三标准入口)为准。

| 目的 | 标准入口 | 覆盖范围与限制 |
|---|---|---|
| 模块常规验证 | `make test-module MODULE=transfer` | 平台 T0、Go T1、前端 T1/T3 与已登记 PostgreSQL T2；不代表真实 Oracle/Kafka 数据面或完整 T4。 |
| Go 开发定位 | `make test-module MODULE=transfer GO_ONLY=1` | 仅 owner Go T1，不含前端、PostgreSQL T2 或真实 Online 验收，不计为完整模块门禁。 |
| PostgreSQL 持久化专项 | `make test-transfer-postgres` | schema、执行读取隔离、事件与 fencing、自动恢复与退避、输出保护和目标覆盖等回归；无需 PostGIS，不运行 Oracle CDC 矩阵。 |
| Hosted ETL 场景入口回归 | `make test-transfer-online-runner` | 身份、只读预检、SQL ETL 场景与清理契约回归，另检查浏览器脚本语法；不启动真实 Hosted suite。 |
| Hosted 连续执行入口回归 | `make test-transfer-continuous-online-runner` | 生命周期、事件、故障与清理契约的确定性检查；不等于真实 Kafka/CDC 或浏览器链路已运行。 |

PostgreSQL 门禁及模块聚合的 T2 步骤要求已核对的 `ADDP_TEST_POSTGRES_PORT`、`ADDP_TEST_POSTGRES_PASSWORD` 等测试连接。先用 `bash scripts/infra/status.sh` 核对实际映射，凭据通过受控环境注入，不写入命令字面值或日志。本模块本地使用 `addp_test`；库归属、权限与清理边界见[Infra PostgreSQL 清单](../scripts/infra/README.md)和[测试隔离规范](../docs/spec/addp测试与验收规范.md)，不创建旁路测试库。T2 前提缺失会在模块门禁执行前拒绝，不能把只完成 Go 子集算作模块通过。

### ArcGIS 开放格式专项

```bash
make test-arcgis-open-formats
```

原生 GDAL/Python、可达的 GeoPython Runtime、Business Oracle、样本路径和部分运行模式统一见[脚本专题](../scripts/docs/测试与发布入口.md#arcgis-开放格式集成门禁)；门禁不启动或重启服务。它断言普通 Access 不误判、PGeo catalog/行读取、PGeo 写入 Oracle 的行数/Point/MultiPolygon geometry/SRID（含无 SRID 源数据），以及 Oracle Spatial 写入 FileGDB 后回读的 123 行 MultiPolygon、非空 geometry 和 SRID 0。测试只使用临时 Oracle 表和临时 `.gdb` 目录，结束自动清理；运行账号须有对应临时对象的创建与清理权限，并核对实际清理结果。

### Oracle CDC 专项入口缺口

以下显式开关用例尚未在根 Makefile、`scripts/test/` 或 Online/Release suite 中登记专门的调用与隔离清理门禁。现有实现与环境前提保留如下；本页不再提供直接语言命令作为执行入口，待测试 Owner 补齐标准门禁及 CI 接线后再提供正式调用示例。

| 专项实现 | 当前覆盖与环境前提 |
|---|---|
| [Oracle Spatial CDC 几何与恢复](backend/internal/continuous/oracle_spatial_cdc_integration_test.go)，`ADDP_ORACLE_SPATIAL_CDC_DATA_E2E=1` | Oracle Spatial XY Polygon/MultiPolygon 到 PostgreSQL/MySQL/Oracle，以及 XYZ Point 到 PostgreSQL；需要 Business Oracle、相应目标数据库、Kafka Connect、Infra Kafka 与 `addp_test`。 |
| [Oracle 目标生命周期](backend/internal/continuous/oracle_target_cdc_integration_test.go)，`ADDP_DATABASE_CDC_ORACLE_TARGET_E2E=1` | 当前循环只有 PostgreSQL、MySQL 两类源到 Oracle 目标，不作为 Oracle 源场景；需要对应 Business 源/目标、Kafka Connect、Infra Kafka 与 `addp_test`。 |

未启用上述开关时，用例会跳过；普通模块或 Go 门禁成功不能计为这两类实测通过，也不应向常规 Go 门禁注入专项开关以替代隔离入口。`ADDP_ORACLE_SPATIAL_CDC_CONTAINER_FAULT=1` 会额外暂停并恢复 `business-oracle` 容器，只适用于独占且已授权的故障窗口；普通回归不启用。用例结束预期自动 unpause，仍须核对服务恢复与清理结果。

这些格式和低层数据面用例不替代真实 User → System → Gateway → Owner／Worker 授权链路或浏览器验收；T4 必须沿[正式 Online 身份与隔离规范](../docs/spec/testing/T4-Online验收规范.md)，使用已登记 suite 的专用身份、最小权限和独占部署，不复用个人开发服务或扩大生产权限。固定版本证据、失败与修复结论及未覆盖项见[模块验证](CLAUDE.md#开发与验证)，未运行、失败、跳过或未覆盖项不计为通过。

## 相关文档

- [Transfer 模块说明](./CLAUDE.md)
- [Transfer 当前架构设计](./docs/design.md)
- [当前支持矩阵与任务语义](./docs/transfer-任务语义与同步模式.md#三当前支持矩阵)
- [Transfer 后续能力清单](../docs/next/transfer后续能力清单.md)
- [Transfer 基本概念及配置说明](./docs/transfer-基本概念及配置说明.md)
- [table Transfer 配置](./docs/transfer-基本概念及配置说明.md#三table-transfer-支持范围)
- [raw copy 配置](./docs/transfer-基本概念及配置说明.md#四raw-copy-支持范围)
- [Continuous/Kafka 配置](./docs/transfer-基本概念及配置说明.md#八continuouskafka-v1-契约与当前实现边界)
- [数据库 CDC 配置](./docs/transfer-基本概念及配置说明.md#九数据库-cdc-v1-已冻结边界)
- [Checkpoint 恢复分级](./docs/transfer-基本概念及配置说明.md#恢复分级)
- [Transfer 数据库架构](./docs/数据库架构.md)
- [DLQ 公开查询与来源隔离](./docs/数据库架构.md#dlq-公开查询与来源隔离)
- [DLQ availability 失败与验收边界](./docs/数据库架构.md#availability-失败与验收边界)
- [transfer_tasks 表](./docs/tables/tasks表.md)
- [task_executions 表](./docs/tables/task_executions表.md)
