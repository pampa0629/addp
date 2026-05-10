# Common Engine 引擎插件后续事项

更新时间：2026-05-08

本文是 `common/engine` 引擎插件相关工作的唯一 next 接力文档。已转正的概念以以下正式规范为准：

- `docs/spec/addp引擎插件接口规范.md`
- `docs/spec/addp引擎能力声明规范.md`
- `docs/spec/addp存储引擎路径体系规范.md`
- `docs/spec/addp路径统一和指纹计算.md`

---

## 一、当前状态

已完成内容只保留摘要，避免后续接力时继续翻历史 checklist。

- `common/engine` 核心接口、能力声明、CatalogProvider、DSN provider、provider validator、主要插件和上层调用方已按新规范完成迁移。
- 旧接口和旧字段已从代码主路径移除：`EngineCategory()`、`BuildConnectionString()` 基础接口、`storage.families`、`store.read/write/random_write/transactions/formats` 等。
- Store 能力已收敛为 `stream_read`、`stream_write`、`range_read`、`range_write`、`batch_read`、`batch_write`。
- `EngineOrigin()` 已替代 `EngineCategory()`，取值为 `general` / `extension`。
- `connection_info` map 是所有引擎连接信息事实源；数据库类插件通过可选 `DSNProvider.BuildDSN()` 提供 driver DSN。
- Catalog 拼装已按 family 拆分：MinIO / S3 使用 object catalog，NFS 使用 file catalog，MongoDB 使用 document catalog，Neo4j 使用 graph catalog。
- MinIO / S3 / NFS 的 Store 能力已按真实 Provider 收紧；MinIO / S3 已实现并声明 `range_read`，未声明未实现写能力。
- NFS 当前以 `name="."` 的唯一 root 节点容纳挂载根目录下文件；后续重构必须保留这个外在行为。
- Catalog / Metadata / TestConnection 已按只读原则收敛；PostgreSQL catalog 查询已移除 `ANALYZE` 外部副作用。
- MongoDB / Neo4j 已改用能力 builder，MongoDB 查询语言已统一为 `mql`。
- capabilities validator 已覆盖插件注册、CatalogModel 一致性、Store / Query / Workflow / Script 能力声明与 Provider 实现一致性。
- System 后端已生成 `capabilities_view`，引擎详情页已改为能力摘要、能力卡片、目录链路、扩展区和“查看 JSON”树形视图。
- Format Registry 第一阶段已落地，`transfer.supported_formats` 已从各引擎能力 builder 手写清单改为按引擎家族派生。
- 上层模块硬编码收口 P0-P3 已完成第一阶段：
  - Manager 对象/文件预览、数据库预览、Feature/GeoJSON/MVT、Service 查询执行已分别收口到 Provider、`dbbridge`、`common/spatial`、`common/sqldialect` 等 common helper。
  - Develop Notebook 数据源注入已改为 Develop 内部 runtimeconn 适配层派生连接描述；DuckDB 联邦挂载判断已集中到 `common/duckdb`。
  - 引擎展示分类/图标已优先从 capabilities / catalog model 派生，`engine_type` 仅作兜底。

---

## 二、剩余工作

### 1. 上层模块残留引擎类型硬编码收口

第一阶段已完成。完成范围只保留清点，避免在 next 文档中堆积已结束任务：

- Manager 对象/文件读取主路径已 Provider 化，私有 `EngineConnector` 已删除。
- Manager PostgreSQL/PostGIS 专用空间路径已集中到 `common/spatial`；通用 SQL 预览/查询拼装已集中到 `common/sqldialect`。
- Develop Notebook 连接注入已集中到 Develop 内部 runtimeconn 适配层；DuckDB 联邦挂载判断已集中到 `common/duckdb`。
- 展示用途的引擎分类和图标已优先从 capabilities / catalog model 派生。

仍需保留的边界：

- Transfer 执行面、表单创建入口和明确业务模式选择可以保留必要类型选项；若要继续收口，需要先让对应模块 API 输出能力派生的可选项。
- DuckDB `ATTACH` / `httpfs` 差异属于 DuckDB 适配层真实差异，暂不进入通用 DSN 或 engine plugin Provider。
- `common/sqldialect` 目前是普通 helper；Notebook runtimeconn 属于 Develop 内部适配层。若未来上升为 Provider，先补 SQL preview composer / runtime export 能力边界文档。

### 2. 能力边界状态继续精细化

目标：更稳定地区分“引擎本身没有”和“ADDP 当前暂未实现”，避免都显示成笼统“不支持”。

当前状态：

- `capabilities_view` 已支持 `available`、`engine_unavailable`、`addp_pending` 三类状态。
- 对对象存储缺失查询运行时这类明确情况，已可展示为 `engine_unavailable`。

后续工作：

- 梳理各引擎家族的理论能力边界，形成后端展示定义，减少散落判断。
- 对“引擎理论上可做但 ADDP 尚未实现”的 Transfer、Preview、写入等能力，逐步补充 `addp_pending` 展示。
- 不把这些状态强行塞进 `EngineCapabilities` 核心结构，优先作为 `capabilities_view` 派生结果。

### 3. Format Registry 深化

目标：在第一阶段清单集中管理基础上，把格式能力变成跨模块共同事实源。

当前状态：

- `common/format/capability` 已集中声明格式、扩展名、数据类型、Transfer / Preview / Parse 能力和适用引擎家族。
- `common/engine/plugin` 的能力 builder 已按引擎家族从 Format Registry 派生 `supported_formats`。
- 当前保持对外输出不变：表格引擎为 `table`，对象 / 文件引擎为 `csv、geojson、json、parquet、shapefile`，文档引擎为 `document、json`。

后续工作：

- Manager、Transfer、Meta 逐步消费 Format Registry，减少各模块独立维护格式清单。
- 将最终支持格式从“引擎家族映射”演进为“引擎访问能力 × Format Registry × Transfer / Preview 实现”的完整推导结果。
- `transfer.supported_formats` 在迁移完成前阶段性保留；迁移后评估改为纯派生展示或模块能力结果。

### 4. SQL metadata 方言继续收敛

PostgreSQL、ClickHouse、Spark SQL 的 metadata 查询仍可继续抽成方言 helper。当前重复面已从连接、DSN、pool 和 MySQL-compatible metadata 层收敛，不影响新规范落地。

### 5. 文档同步检查

如后续修改能力展示 API 或能力字段，需要同步检查：

- `manager/docs/数据预览API重构方案.md`
- `system/docs/tables/engines表.md`
- `docs/spec/addp引擎能力声明规范.md`
- `docs/spec/addp引擎插件接口规范.md`

---

## 三、验证建议

每轮修改 `common/engine` 或引擎插件后至少执行：

```bash
go test ./common/engine/plugin ./common/engine/plugins/...
go test -tags integration ./common/engine/plugin/integration -run '^TestPluginInterfaceImplementation'
git diff --check
```

修改 Format Registry 后执行：

```bash
go test ./common/format/capability ./common/format ./common/engine/plugin ./common/engine/plugins/...
```

修改 System 能力展示后执行：

```bash
go test ./system/backend/internal/service ./system/backend/internal/api
cd system/frontend && npm run build
bash scripts/swagger/gen-swagger.sh system
bash scripts/swagger/check-route-coverage.sh system
git diff --check
```

如果修改 `common/` 共享能力并需要运行时验证，按项目规范使用：

```bash
./scripts/dev/restart.sh -all
```

---

## 四、暂不建议直接做的事

- 不建议在前端按原始 capabilities JSON 硬编码展示规则。
- 不建议在主展示区直接展示原始 JSON 文本。
- 不建议把 `supported`、`schema_version`、`path_version` 等技术字段作为用户主信息展示。
- 不建议提前声明未实现能力。
- 不建议继续为新增 SQL 引擎复制整文件。
- 不建议恢复旧能力结构或旧路径语义兼容层。
