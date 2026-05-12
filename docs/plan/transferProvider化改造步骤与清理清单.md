# Transfer Provider 化改造步骤与清理清单

更新时间：2026-05-09

本文给出 Transfer 接入统一 Provider 体系的执行顺序。目标是最终删除旧 connector type 路由、旧 reader / writer 混合逻辑、旧 API 和旧数据口径，不保留兼容负担。

## 总目标

Transfer 最终应做到：

```text
任务配置 / Meta item
  -> TransferPlanner
  -> engine capability + resource abstraction
  -> FormatPlugin + info provider / content reader
  -> pipeline.Reader / pipeline.Writer
  -> 执行、提交、指标、日志
```

上层不再直接选择 `s3_shapefile`、`spatialite_parallel`、`postgres_copy` 这类混合 connector。

## 阶段 0：冻结旧扩展方式

目的：避免继续扩大历史债务。

要求：

- 不再新增以具体 engine + format 组合命名的 connector。
- 不再新增 `s3_xxx`、`nfs_xxx` 这类组合 reader / writer。
- 新增格式能力优先进入 `common/format` provider。
- 新增引擎访问能力优先进入 engine provider 或 `common/resource` adapter。
- Transfer 只新增 planner / adapter / strategy。

## 阶段 1：补 TransferPlan 草案实现

新增 Transfer 内部 planner，不直接替换所有 reader / writer。

建议新增位置：

```text
transfer/backend/internal/planner/
  endpoint.go
  plan.go
  planner.go
  capability_check.go
  config_normalizer.go
```

第一版职责：

1. 解析 task config 中的 source / target。
2. 区分 engine、resource、data_type、format、spatial、policy。
3. 查询 System / local engine 配置。
4. 生成 `TransferPlan`。
5. 给当前 `pipeline.ConnectorConfig` 生成兼容 adapter 输入。

这一阶段可以不改执行结果，但要把旧推断逻辑从 `ExecutionEngineService` 中抽出来。

待迁移旧逻辑：

- `resolveConnectorConfig()`
- `resolveSystemEngine()`
- `resolveLocalEngine()`
- `resourceToConnectorConfig()`
- `inferConnectorType()`
- S3 / NFS 字段映射逻辑
- PostgreSQL 自动选择 `postgres_copy`
- S3 target 自动注入空间 transform

## 阶段 2：引入资源读写 adapter

目标：Transfer 不再让格式 reader / writer 直接创建 S3 / NFS / local 访问。

建议新增：

```text
transfer/backend/internal/resourceadapter/
  reader_factory.go
  writer_factory.go
  native_table_factory.go
```

读取侧：

- system / local engine -> `common/resource.ResourceReader`
- multi 组件 -> `common/resource.ComponentReader`
- engine-native table -> native cursor / batch reader adapter

写入侧：

- object / file target -> resource write session
- multi component target -> component write session
- engine-native table -> native batch writer

第一阶段如果 `common/resource` 还没有写入抽象，可先在 Transfer 内部定义 `ResourceWriteSession`，稳定后再沉淀到 common。

## 阶段 3：table batch provider 对齐

目标：让 table 型传输主路径基于 info provider / content reader，而不是格式 connector。

优先场景：

| 场景 | 目标 |
|---|---|
| PostgreSQL table -> PostgreSQL table | engine-native read/write |
| PostgreSQL table -> CSV | native read + CSV format write + resource write |
| CSV -> PostgreSQL table | resource read + CSV format read + native write |
| Parquet scope -> PostgreSQL table | scope read + Parquet format read + native write |
| PostgreSQL table -> Parquet | native read + Parquet format write + resource write |
| Shapefile -> PostgreSQL table | component read + Shapefile format read + native write |

需要新增或补齐的 format 能力：

- CSV `ReadBatch` / `WriteBatch`
- JSON `ReadBatch` / `WriteBatch`
- Parquet `ReadBatch` / `WriteBatch`
- Shapefile `ReadBatch` / `WriteBatch` / component write

当前 `common/format.TableProvider` 已有 `DescribeTable` / `SampleTable`，Transfer 需要在此基础上扩展批量读写能力，或先在 Transfer 内通过 adapter 包一层过渡接口。

## 阶段 4：提交策略与 checkpoint 收口

目标：解决批量写出、并行、失败恢复的边界问题。

新增 plan 内部模型：

- `CommitPolicy`
- `CheckpointPolicy`
- `ParallelPolicy`
- `StagingPolicy`

必须明确：

- 单文件写出是否先写 staging 再提交。
- 多组件写出是否整体提交。
- scope 目录写出是否替换整个 scope。
- 数据库写入是否使用 transaction / truncate+insert / copy。
- checkpoint 的恢复粒度是 record、file、row group、partition 还是 cursor。

旧的单一 `checkpoint_offset` 可继续作为执行记录中的摘要字段，但真实 checkpoint state 应进入结构化 JSON。

## 阶段 5：前端任务向导与 API 配置调整

目标：用户配置不再暴露旧 connector 混合口径。

前端应从：

- 选择 source connector。
- 选择 target connector。
- 选择 output format。

调整为：

- 选择 source engine / resource / item。
- 选择 target engine / resource。
- 选择 data type。
- 选择 format。
- 配置 schema / mapping / spatial / write policy。

任务 API 应承接目标 endpoint 模型。

注意：这一步会影响数据库任务配置，代码侧不需要保留旧数据兼容。按用户确认，本阶段可以通过清空旧任务或迁移脚本直接删除旧结构。

## 阶段 6：删除旧 connector 注册和旧读写器

当新 planner 覆盖主路径后，删除旧入口。

优先删除或改造：

| 旧对象 | 处理方式 |
|---|---|
| `s3_shapefile` connector | 删除，改为 S3 ResourceReader + Shapefile provider |
| `postgres_copy` connector | 删除，改为 PostgreSQL write strategy |
| `spatialite_parallel` connector | 删除，改为 read strategy |
| `geojson` connector | 删除，改为 `format=json + spatial.encoding=geojson` |
| `parquet` 中的 S3 访问逻辑 | 删除，改为 ResourceReader + Parquet provider |
| `S3Reader` 的 CSV/JSON 解码分支 | 删除，改为 ResourceReader + FormatPlugin / content reader |
| `S3Writer` 的 CSV/JSON/Shapefile/Parquet 分支 | 删除，改为 format writer + resource writer |
| `NFSWriter` 中的格式分支 | 删除，改为 resource writer |
| `inferConnectorType()` | 删除 |
| `resourceToConnectorConfig()` | 删除或改为 planner 的 engine adapter |

保留但调整定位：

- `pipeline.ExecutionEngine`
- `pipeline.ParallelExecutionEngine`
- `pipeline.Reader`
- `pipeline.Writer`
- `pipeline.Transform`
- `pipeline.DataBatch`
- `TransformRegistry`

## 历史问题一并清理

### mode 字段断裂

旧文档 `engine-plugin-transfer后续事项.md` 记录过：

- 前端任务向导仍可能提交 `mode`。
- pipeline 有 `ModeBatch`、`ModeStream`、`ModeMicroBatch`。
- 后端任务模型未完整承接 mode。
- `ExecutionEngineService.buildExecutionTask` 当前固定 `pipeline.ModeBatch`。

清理时有两条路：

1. 如果 Transfer 第一阶段只做批处理，则删除前端流式 / 微批入口、旧测试预期和旧文档描述。
2. 如果要保留模式概念，则恢复 `TaskMode` 领域枚举，并把它纳入 `TransferPlan.Execution.mode`。

不建议只修测试或只补字段。mode 是执行策略，应归入 planner。

### `geojson` 口径

Transfer 中的 GeoJSON 只能表示 JSON 空间编码或输出内容结构。

清理目标：

- 删除顶层 `connector_type=geojson`。
- 删除顶层 `format=geojson`。
- 使用 `format=json`。
- 使用 `spatial.target_encoding=geojson`。

### 空间字段默认值

清理目标：

- 删除默认 `geometry_field=geometry` 或 `geom` 的隐式业务假设。
- 优先使用 Meta attributes / source schema。
- 用户高级配置只能作为覆盖。

## 测试门禁

每阶段至少保持：

```bash
go test ./common/resource ./common/format/... ./common/engine/plugin
go test ./transfer/backend/pkg/pipeline ./transfer/backend/internal/service
npm run build --prefix transfer/frontend
git diff --check
```

当改动 Transfer API：

```bash
bash scripts/swagger/gen-swagger.sh transfer
bash scripts/swagger/check-route-coverage.sh transfer
```

当改动 common：

```bash
./scripts/dev/restart.sh -all
```

当只改 Transfer：

```bash
./scripts/dev/restart.sh -transfer
```

## 建议执行顺序

1. 先做 `TransferPlan` 文档到代码的最小结构，不改变执行行为。
2. 把 `ExecutionEngineService` 中的配置推断迁入 planner。
3. 用 planner 输出继续驱动旧 registry，确认行为不变。
4. 为 CSV 单文件读写接入 resource + FormatPlugin / content reader adapter。
5. 为 Parquet scope 读接入 resource + FormatPlugin / content reader adapter。
6. 为 Shapefile multi 读写接入 component reader / writer。
7. 把 PostgreSQL COPY 改成 native write strategy。
8. 删除被新路径覆盖的旧 connector。
9. 改前端向导和任务 API。
10. 清空旧任务数据，重建样例任务，补端到端验证。

## 暂不纳入第一轮

- 实时流处理。
- Kafka / CDC。
- 非 table data type 的完整传输。
- Iceberg / Delta / Hudi 写入。
- 跨任务增量同步语义。

这些能力不否定目标模型，但不应阻塞 table 批量读写主路径收口。
