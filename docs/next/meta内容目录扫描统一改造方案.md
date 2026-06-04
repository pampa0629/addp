# Meta 内容目录扫描统一改造方案

本文记录 Meta 对 object catalog、file catalog、refs group、scan scope 和 item persist 主链路的统一改造方案。本文属于 `docs/next` 工作方案，不直接替代正式规范；进入实现前，应同步修订术语表和相关 `docs/spec/` 规范。

## 背景

当前 Meta 已经具备以下基础能力：

- common engine 层已经用 `CatalogModel`、`CatalogProvider`、`CatalogFactsProvider`、`ContentReadableProvider` 统一表达 object catalog 与 file catalog。
- `common/dataitem.ResolveItems` 与 `meta/internal/metaitem.ResolveItems` 已经具备 multi / whole / single item 识别能力。
- Meta 已有 `catalogSingleItemProcessor`，可以统一处理 single item 的 attributes、deep enrich、content hash、extraction、index 和 upsert。
- 已知 item refresh 已经基于落库 attributes 还原 descriptor，并按 layout 刷新当前 item。

主要问题是：这些能力还没有收敛成统一主链路。object catalog、file catalog、Transfer 触发扫描、item refresh 和任务创建仍在不同位置解析目标、组织 refs、识别 item、构建 attributes 和落库。

## 核心结论

1. object catalog 与 file catalog 在 common engine 层已经统一到 Meta 可消费的接口层，但不应抹平它们的 catalog 语义。
2. Meta 内部应保留 object / file 两个 adapter，合并为一条 `ContentCatalogScanner` 主流程。
3. `catalog_paths` 只能表达路径型 catalog selector，不能表达 refs group。
4. Transfer 完成后应提交本次实际生成的 refs group，由 Meta 在该边界内识别 item。
5. `trigger_type` 只保留 `manual` / `scheduled`；Transfer、Manager、System 等来源应进入 `source` 或 execution metadata。
6. item detection、deep enrich 和 persist 应形成统一主路径，旧的分支式后验合并、软删 sidecar 路径应被删除。

## 概念边界

### ScanSelector

`ScanSelector` 表示 API 层或模块调用方提交的扫描选择器。

可包含：

- engine selector：`engine_id`
- node selector：`node_id`
- item selector：`item_id`
- locator selector：`targets`
- catalog path selector：`catalog_paths`
- content refs selector：`ref_groups`

### ScanScope

`ScanScope` 是 Meta 内部唯一扫描范围模型。所有 selector 进入扫描主链路前必须先解析为 `ScanScope`。

建议内部字段：

```go
type ScanScope struct {
	EngineID     uint
	Mode         ScanScopeMode
	CatalogPaths []plugin.CatalogPath
	RefGroups    []ScanRefGroup
	Source       string
	ScanDepth    string
	Force        bool
}
```

### ScanRefGroup

`ScanRefGroup` 表示一组共同参与 item 识别的内容引用，不绑定 ADDP locator。

建议内部字段：

```go
type ScanRefGroup struct {
	Primary string
	Refs    []ScanRef
}

type ScanRef struct {
	Path string
	Role string
	Required bool
}
```

`ScanRef.Path` 可以来自 catalog path、content ref、provider ref 或 Transfer 执行结果。API 层可以接受字符串路径，内部必须转成 engine 对应的 `plugin.CatalogPath` 或 content ref。

## API 调整方向

Meta 对外保留少数稳定入口：

1. `POST /scan/run/manual`
   - 创建异步扫描运行。
   - 支持 engine / node / item / locator / catalog paths / ref groups。
   - 所有耗时扫描都进入后台执行。

2. `POST /items/{item_id}/refresh`
   - 同步刷新已知 item。
   - 只刷新当前已入库 item 的 metadata facts。
   - 不重新裁决 item 边界，不枚举父目录。

3. 查询接口保持现状
   - tree、node、item、fields、spatial、by-catalog-path 等查询 API 不进入本轮主改造。

建议扫描请求结构：

```json
{
  "engine_id": 1,
  "node_id": 0,
  "item_id": 0,
  "targets": [],
  "catalog_paths": [],
  "ref_groups": [
    {
      "primary": "bucket/path/roads.shp",
      "refs": [
        {"path": "bucket/path/roads.shp", "role": "main", "required": true},
        {"path": "bucket/path/roads.shx", "role": "sidecar", "required": true},
        {"path": "bucket/path/roads.dbf", "role": "sidecar", "required": true}
      ]
    }
  ],
  "scan_depth": "deep",
  "force": false,
  "trigger_type": "manual",
  "source": "transfer"
}
```

约束：

- `trigger_type` 只允许 `manual` / `scheduled`。
- `source` 用于记录触发来源，例如 `system_immediate`、`manager_refresh`、`meta_frontend`、`transfer`。
- `catalog_paths` 只表示 engine catalog model 下的路径。
- `ref_groups` 只表示内容引用边界。
- 同一批 Transfer 生成物不得同时用父目录 `catalog_paths` 和 `ref_groups` 表达。

## 内部架构目标

目标主链路：

```text
API request
  -> ScanSelector
  -> ScanScopeResolver
  -> ScanScope
  -> strategy / adapter
  -> ContentCandidateSet / RefGroupCandidateSet
  -> metaitem.ResolveItems
  -> DetectedItem
  -> DetectedItemProcessor
  -> metaenrich
  -> metaattr
  -> repository.UpsertItemWithDepth
```

## object / file catalog 统一方式

不把 object catalog 和 file catalog 合成同一个 common engine 模型。它们的 root、branch 和 leaf 术语必须保留：

- object catalog：`service -> bucket -> prefix? -> object`
- file catalog：`root -> directory? -> file`

Meta 内部新增统一 scanner 和两个 adapter：

```go
type ContentCatalogAdapter interface {
	Model() plugin.CatalogModelSpec
	LeafTerm() string
	CatalogPathFor(path string) plugin.CatalogPath
	RootName(entry plugin.CatalogEntry) string
	EntryPath(entry plugin.CatalogEntry) string
	ParentNodePlan(scope ScanScope, entry plugin.CatalogEntry) ParentNodePlan
	CandidateFromEntry(entry plugin.CatalogEntry) ContentCandidate
}
```

object adapter 负责：

- bucket / prefix / object 路径语义；
- `storage.bucket`；
- object item 的 `item_type=object`；
- bucket/prefix node 聚合；
- object catalog path 与 object content path 的转换。

file adapter 负责：

- root / directory / file 路径语义；
- 文件路径归一化；
- file item 的 `item_type=file`；
- directory node 聚合；
- file catalog path 与 filesystem content path 的转换。

统一 `ContentCatalogScanner` 负责：

- scope 枚举；
- candidate 组织；
- refs group 扫描；
- `metaitem.ResolveItems`；
- claims / exclusive 处理；
- single fallback；
- deep enrich；
- content hash；
- document extraction；
- index；
- persist / upsert；
- scanned depth 和 force 策略。

## Persist 收敛方向

现有 `catalogSingleItemProcessor` 应升级为通用 `DetectedItemProcessor`。

统一处理：

- attributes 初始化与 normalize；
- `metaattr.MergeDataItemAttributes`；
- storage facts 写入；
- deep enrich；
- access index；
- content hash；
- extraction；
- search index；
- row count / size bytes；
- scanned depth；
- `repository.UpsertItemWithDepth`。

single / multi / whole item 不应分别拥有三套落库路径。差异应体现在 `DetectedItem` 和 adapter 提供的 parent node / path plan 中。

## Transfer 改造方向

Transfer 的边界：

1. 执行数据传输。
2. 记录本次实际写出的 content refs。
3. 调用 Meta `POST /scan/run/manual`，提交 `ref_groups`。

Transfer 不应：

- 判断 refs 是否构成 data item；
- 调用 `common/dataitem` 识别 item；
- 为了触发识别而扩大到父目录；
- 生成多个 sidecar item 后要求 Meta 合并。

Shapefile 输出示例：

```json
{
  "engine_id": 9,
  "ref_groups": [
    {
      "primary": "manager/a5.shp",
      "refs": [
        {"path": "manager/a5.shp", "role": "main", "required": true},
        {"path": "manager/a5.shx", "role": "sidecar", "required": true},
        {"path": "manager/a5.dbf", "role": "sidecar", "required": true},
        {"path": "manager/a5.cpg", "role": "sidecar", "required": false},
        {"path": "manager/a5.prj", "role": "sidecar", "required": false}
      ]
    }
  ],
  "scan_depth": "deep",
  "force": true,
  "trigger_type": "manual",
  "source": "transfer"
}
```

## 改造顺序

### 阶段 0：规范同步

必须先修订：

- `docs/concepts/addp术语表.md`
- `docs/spec/addp元数据扫描机制规范.md`
- `docs/spec/addp数据项探测器规范.md`
- 必要时修订 `docs/spec/addp存储引擎路径体系规范.md`

明确：

- `catalog_paths` 是路径型 selector。
- `ref_groups` 是内容引用边界。
- `trigger_type` 与 `source` 分离。
- object / file catalog 可以共用内容目录扫描主链路，但保留 catalog model 术语差异。

### 阶段 1：API 与 DTO

- Meta `ScanRequest` 增加 `RefGroups` 和 `Source`。
- common MetaClient 增加 `RefGroups` 和 `Source`。
- Swagger 注解和产物同步。
- 校验 `trigger_type` 只允许 `manual` / `scheduled`。

### 阶段 2：ScanScopeResolver

- 新增统一 resolver。
- 删除 `ScanService` 与 `ScanTaskService` 中重复的 target 到 `catalog_paths` 解析。
- task execution config 存储统一 scope，不再只存 `catalog_paths`。

### 阶段 3：refs group scan path

- 支持给定 refs group 直接构造 candidate set。
- 不枚举父目录。
- 进入 `metaitem.ResolveItems`。
- 统一 persist。

### 阶段 4：ContentCatalogScanner

- 抽出 object / file adapter。
- 将 object 和 file 的复合 item detection、single fallback、persist、deep enrich 迁入统一 scanner。
- 删除 object / file 分支中重复的 item 落库和后验合并逻辑。

### 阶段 5：Transfer 回扫

- Transfer 记录本次实际输出 refs。
- Transfer 调用 Meta 时提交 `ref_groups`。
- 删除 multi / whole 格式扩大到父目录的 `targetCatalogPaths` 逻辑。

### 阶段 6：测试与验证

至少补充：

- `ScanScopeResolver` 单元测试。
- `ref_groups` API / MetaClient 测试。
- object refs group 识别 Shapefile 测试。
- file refs group 识别 Shapefile 测试。
- Transfer 输出 Shapefile 后 Manager 只看到一个 logical item 的集成测试。

验证命令：

```bash
go test ./common/dataitem/... ./common/engine/plugin/... ./meta/backend/internal/metaitem/... ./meta/backend/internal/service/... ./transfer/backend/internal/service/... ./transfer/backend/internal/planner/...
bash scripts/swagger/gen-swagger.sh meta
bash scripts/swagger/check-route-coverage.sh meta
```

## 完成标准

- Meta API 有一等 `ref_groups` 输入。
- Transfer 不再用父目录 `catalog_paths` 表达本次生成物 refs。
- object catalog 和 file catalog 共用 `ContentCatalogScanner` 主链路。
- object/file adapter 只保留 catalog model、路径、node plan、storage 语义差异。
- item detection 统一进入 `metaitem.ResolveItems`。
- single / multi / whole item 统一进入 `DetectedItemProcessor`。
- 不再存在先生成 sidecar item 再合并、软删的主路径。
- Shapefile Transfer 输出后 Manager 中只出现一个 Shapefile item。
