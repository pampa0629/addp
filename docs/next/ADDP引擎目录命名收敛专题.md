# ADDP Engine Catalog 命名收敛与迁移专题

更新时间：2026-08-26

状态：阶段 0–3 已完成；阶段 4 定向验证已通过，平台全量门禁受工作树中其他未完成改造阻塞

## 一、文档定位

本专题跟踪 ADDP 引擎层 `catalog` 词族向 `Engine Catalog` 词族收敛的架构决策、影响范围、迁移批次和验收结果。

本专题只负责引擎目录命名收敛，不设计企业资源目录的数据模型、业务元数据或资产发布流程。企业资源目录目标架构和实施计划见 [ADDP 企业资源目录与 Catalog 模块专题](ADDP企业资源目录能力专题.md)。

两项工作的先后关系固定为：

```text
Engine Catalog 命名收敛
    → 企业 Catalog 正式术语固化
    → 企业 Catalog 对象与 API 设计
    → 企业 Catalog 模块实现
```

引擎目录命名未完成前，不创建企业 Catalog 的同名跨模块 DTO、数据库模型或 API Schema。

## 二、问题与决策

ADDP 当前同时需要表达两种不同的 Catalog：

1. 引擎实时目录：用于枚举数据库、Schema、表、Bucket、目录、对象、Collection、Graph 和 Topic，并读取引擎原生事实；
2. 企业资源目录：用于建立企业级稳定身份，关联业务语义、责任、治理状态和专业资源。

迁移前引擎层大量使用裸名 `CatalogProvider`、`CatalogPath`、`CatalogEntry` 和 `CatalogFacts`。企业资源目录也天然需要 `Catalog` 与 `CatalogEntry`，继续复用裸名会造成概念、代码搜索、DTO、Swagger Schema 和模块职责混淆。

已经确认的单一路线是：

- 引擎层正式名称为 **Engine Catalog / 引擎目录**；
- 引擎层跨模块代码类型统一使用 `EngineCatalog*` 前缀；
- 不带限定词的 **Catalog / CatalogEntry** 保留给企业资源目录；
- Meta 扫描结果继续使用 `node`、`item` 和 `DataItem` 词族；
- Manager 展示继续使用 `resource tree`，不把视图名称改成 Catalog；
- 不采用 `Namespace`、`Resource`、`Inventory`、`Discovery` 或 `Hierarchy` 替代整个引擎目录概念。

## 三、正式词汇边界

| 语境 | 正式名称 | 回答的问题 | 不负责 |
| --- | --- | --- | --- |
| Engine Plugin | Engine Catalog / 引擎目录 | 当前引擎如何组织和定位可枚举对象、对象有哪些引擎原生事实 | 企业业务语义、治理状态、资产发布 |
| Meta | 技术元数据目录 | 扫描后有哪些 node 和 DataItem、技术事实和当前扫描状态是什么 | 企业目录身份和业务编目 |
| Catalog module | Enterprise Catalog / 企业资源目录 | 企业有哪些资源、是什么意思、由谁负责、如何关联和发现 | 引擎实时枚举和数据内容读取 |
| Manager | Resource Tree / 技术资源树 | 用户如何浏览、定位、预览和使用技术资源 | 企业目录事实所有权 |
| Asset | Asset Directory / 资产目录 | 哪些治理成果已被组合、发布和运营 | 自动扫描和企业资源盘点 |

正式文档中的裸词 `Catalog` 默认表示企业资源目录。表达引擎层时必须写成 `Engine Catalog` 或“引擎目录”；表达数据库标准或产品原生术语时，允许按上下文使用 database catalog、schema、namespace 等原生名称。

## 四、代码命名映射

第一批公共契约按下表原子重命名：

| 当前名称 | 目标名称 |
| --- | --- |
| `CatalogModelProvider` | `EngineCatalogModelProvider` |
| `CatalogModel()` | `EngineCatalogModel()` |
| `CatalogModelSpec` | `EngineCatalogModelSpec` |
| `CatalogLevelSpec` | `EngineCatalogLevelSpec` |
| `CatalogCapability` | `EngineCatalogCapability` |
| `CatalogFactsCapability` | `EngineCatalogFactsCapability` |
| `CatalogProvider` | `EngineCatalogProvider` |
| `CatalogPath` | `EngineCatalogPath` |
| `CatalogSegment` | `EngineCatalogSegment` |
| `CatalogEntry` | `EngineCatalogEntry` |
| `CatalogFactsProvider` | `EngineCatalogFactsProvider` |
| `CatalogFacts` | `EngineCatalogFacts` |
| `CatalogFactsOptions` | `EngineCatalogFactsOptions` |
| `CatalogStorageFacts` | `EngineCatalogStorageFacts` |
| `CatalogRoleBranch` / `CatalogRoleLeaf` | `EngineCatalogRoleBranch` / `EngineCatalogRoleLeaf` |
| `CatalogRootEntry` | `EngineCatalogRootEntry` |
| `DescribeCatalogFacts` | `DescribeEngineCatalogFacts` |
| `CatalogError` / `CatalogErrorKind` | `EngineCatalogError` / `EngineCatalogErrorKind` |
| System `CatalogList*` / `CatalogDescribe*` DTO | System `EngineCatalogList*` / `EngineCatalogDescribe*` DTO |

`common/engine/plugin` 中其他以 `Catalog` 开头的公开常量、helper 和 error kind 同样统一增加 `Engine` 前缀，例如 `CatalogPathVersion` → `EngineCatalogPathVersion`、`CatalogFactsTableInfo` → `EngineCatalogFactsTableInfo`。带有明确形态限定的内部实现名，例如 `ListTabularCatalogChildren`，在阶段 3 按是否仍有歧义决定最终次序，不为机械统一制造难读命名。

公开错误码同步收敛：

| 当前错误码 | 目标错误码 |
| --- | --- |
| `catalog_request_invalid` | `engine_catalog_request_invalid` |
| `notebook_catalog_forbidden` | `notebook_engine_catalog_forbidden` |
| `catalog_entry_not_found` | `engine_catalog_entry_not_found` |
| `catalog_operation_unsupported` | `engine_catalog_operation_unsupported` |
| `catalog_control_plane_failed` | `engine_catalog_control_plane_failed` |
| `catalog_provider_failed` | `engine_catalog_provider_failed` |
| `catalog_timeout` | `engine_catalog_timeout` |

旧错误码不保留兼容映射。

以下名称因上下文已经由路由或父对象唯一限定，第一阶段保留：

- `/api/v1/system/engines/:id/catalog/children`；
- `/api/v1/system/engines/:id/catalog/facts`；
- Engine storage capabilities 下的 JSON 字段 `catalog_model`、`catalog`、`facts`；
- Engine Catalog path 的线上版本值 `catalog.path/v1`；
- Provider 方法 `ListChildren`、`ResolvePath`、`DescribeEngineCatalogFacts` 中的通用动词；
- 具体数据库或厂商使用的原生 catalog / namespace 术语。

如果实现检查发现这些保留名称在脱离父上下文后会成为公开歧义，再先修订本专题和正式规范，不能边实现边追加例外。

## 五、已知冲突范围

2026-08-26 初次盘点显示，`CatalogProvider`、`CatalogPath`、`CatalogEntry`、`CatalogFacts`、`CatalogModelSpec` 等词族分布在约 331 个代码或文档文件中。该数字只用于评估规模，实施时必须以 `rg` 实时结果为准，不维护易失真的静态文件清单。

当前至少存在三类不同含义的 `CatalogEntry`：

| 位置 | 当前含义 | 目标处理 |
| --- | --- | --- |
| `common/engine/plugin` | 引擎实时目录条目 | 重命名为 `EngineCatalogEntry` |
| `system/backend/internal/models` | System 引擎实时目录 API DTO | 重命名为 `EngineCatalogEntry` 词族 |
| `asset` 与 `common/client/asset.go` | 资产分类目录树节点 | 重命名为明确的 Asset 分类树名称；后续由企业 Catalog 改造删除旧来源路线 |

不能只修改 `common/engine/plugin`，把其他裸名留给调用方自行理解。迁移完成后，仓库中每个剩余的 `CatalogEntry` 都必须确实表示企业目录条目；企业 Catalog 尚未实现前，正常结果应当是生产代码中不存在裸名 `CatalogEntry`。

## 六、迁移原则

1. **文档先行**：先更新术语表和正式规范，再修改公共代码契约。
2. **一次改名**：旧类型、type alias、包装接口和兼容 JSON Schema 不保留。
3. **公共契约原子迁移**：`common/engine/plugin` 与所有直接消费者必须在同一轮代码变更中恢复编译，不能留下中间双轨版本。
4. **语义不扩张**：本轮只改命名和由命名直接导致的 DTO、错误码、文档，不顺便改变目录层级、扫描算法或 Provider 行为。
5. **生成物同步**：Swagger、生成代码、i18n、测试快照和文档示例随 owner 源文件一起更新。
6. **保留原生术语**：数据库 catalog、schema、对象存储 bucket、文件目录和 Kafka topic 等引擎原生术语不做机械替换。
7. **完整验证**：`common` 公共接口变更按真实消费者扩散测试，不以“只是重命名”为由缩小门禁。

## 七、分阶段跟进清单

### 阶段 0：正式概念固化

- [x] 更新 `docs/concepts/addp术语表.md`，把引擎层词族正式定义为 Engine Catalog；
- [x] 更新 `docs/concepts/addp引擎体系图.md`；
- [x] 更新 `docs/spec/addp引擎插件接口规范.md`；
- [x] 更新 `docs/spec/addp元数据扫描机制规范.md`；
- [x] 更新受影响的 Meta、Manager、System、Develop 概念说明；
- [x] 更新企业资源目录专题，确认裸名 Catalog 和 CatalogEntry 归企业目录；
- [x] 冻结第四节命名映射、公开错误码和保留项。

完成门槛：正式文档只存在 Engine Catalog 与 Enterprise Catalog 两个带明确边界的概念，不再把裸名 CatalogEntry 定义为引擎目录条目。

### 阶段 1：影响面与门禁盘点

- [x] 使用 `rg` 生成实时命中结果并按公共契约、插件实现、模块消费、生成物、测试和文档分类；
- [x] 盘点 Go、Python、前端和 Swagger Schema 中的跨语言影响；前端没有直接消费旧 Go/Python 类型；
- [x] 单独识别 Asset 分类目录、System API DTO 等同名但不同义对象；
- [x] 确认根 `Makefile` 的 `test-changed` 能把 `common` 变更扩散到全部实际消费者；`changed-gate.py` 通过 `go.mod` 反向发现消费模块；
- [x] 确认 CI、构建和镜像自动发现依赖模块目录与登记入口，不依赖旧类型名或文件名；
- [x] 记录阶段 2 必须同时恢复编译的 owner 集合：Common、System、Meta、Manager、Develop、Transfer、Service、Quality、Asset、Common-Python 与 Copilot。

完成门槛：迁移范围和最小充分 T0-T3 门禁明确，不把已知编译或生成物问题留到远端 CI 发现。

### 阶段 2：公共契约与消费者原子迁移

- [x] 重命名 `common/engine/plugin` 公共类型、接口、helper 和错误；
- [x] 同步所有内置引擎插件；
- [x] 同步 System 实时引擎目录 API 适配层；
- [x] 同步 Meta 扫描、刷新和 inspect 链路；
- [x] 同步 Manager 预览、内容读取和检索链路；
- [x] 同步 Develop Notebook Native Engine Facade 及 Python SDK；
- [x] 同步 Transfer、Service 和其他真实消费者；
- [x] 将 Asset 分类目录树的歧义类型重命名为 `AssetCatalogTreeNode`；
- [x] 同步单元测试、集成测试和 fixture。

完成门槛：所有产品代码重新编译，旧公共类型没有 alias 或 wrapper，运行行为与改名前一致。

### 阶段 3：内部命名与生成物清理

- [x] 将 Meta `metacatalog` 收敛为 `scanresource`、`CatalogDispatcher` 收敛为 `EngineCatalogScanDispatcher`，Manager `catalogutil` 收敛为 `resourceutil`，并同步通用文件名；
- [x] 更新 Swagger 注解并重新生成 System、Develop、Asset 文档与 Copilot OpenAPI；
- [x] 更新 i18n key、公开错误码、日志和指标标签中的歧义名称；
- [x] 更新 CLAUDE、README、概念图、规范示例和代码注释；
- [x] 检查 API 路由和 capability JSON 保留项；两者仍由 Engine 资源路由或 `storage` 父对象唯一限定；
- [x] 删除生产代码中的旧公共名、alias、wrapper 和过渡说明。

完成门槛：代码搜索结果中不存在代表引擎层的旧裸名 `CatalogProvider`、`CatalogPath`、`CatalogEntry`、`CatalogFacts` 或 `CatalogModelSpec`。

### 阶段 4：全量验证与企业 Catalog 解锁

- [x] 对重命名核心包和各插件运行定向测试；
- [x] 对 System、Meta、Manager、Transfer、Service、Quality、Asset、Portal、Common-Python 和 Copilot 运行模块门禁；Develop 已编译到无 Engine Catalog 遗漏，但被同一工作树中独立的 Materialization Write Context 未完成契约阻塞；
- [ ] 运行 `make test-changed`；
- [ ] 运行 `make test-platform`，确认构建、CI、Swagger 和模块登记一致性；
- [x] 检查 Git diff 中没有兼容分支、旧 JSON 双字段和 fallback；
- [x] 回写本专题的实际验证结果；
- [x] 在企业资源目录专题中解除命名阻塞，企业 Catalog 可继续进入对象与 API 设计。

完成门槛：本地最小充分门禁通过，main push 后现有 CI 能自动覆盖该变更，企业 Catalog 可以安全使用裸名 `CatalogEntry`。

## 八、实施节奏

为了保持工作区持续可判断，采用三个验收点：

1. **文档验收点**：只完成阶段 0，确认术语与映射，不改代码；
2. **代码验收点**：阶段 1 完成后一次执行阶段 2 和阶段 3，不在公共契约破坏状态中停下；
3. **平台验收点**：完成阶段 4 后，才把企业 Catalog 从“命名阻塞”推进到对象设计。

阶段 2 虽然内部可以按插件和模块分批编辑、分批运行定向测试，但对外只能作为一个原子迁移交付，不创建旧名兼容层来换取临时绿色状态。

## 九、当前状态

| 工作项 | 状态 | 说明 |
| --- | --- | --- |
| Engine Catalog 命名方向 | 已确认 | 引擎层使用 `EngineCatalog*`，裸 Catalog 保留给企业目录 |
| 替代词评估 | 已完成 | 不采用 Namespace、Resource、Inventory、Discovery、Hierarchy |
| 初次影响规模 | 已盘点 | 约 331 个文件，实施时以实时 `rg` 为准 |
| 正式概念文档 | 已完成 | Engine Catalog、Enterprise Catalog 及裸名边界已写入正式文档 |
| 公共契约与消费者迁移 | 已完成 | Common 契约、内置插件和全部已知真实消费者已原子收敛 |
| 旧名和生成物清理 | 已完成 | 旧公共名、歧义内部包名、Swagger、OpenAPI 和 i18n 已同步，无兼容路线 |
| 全量验证 | 部分阻塞 | 本专题定向门禁已通过；`make test-platform` 与 Develop 全包测试被工作树中其他未完成改造阻塞 |
| 企业 Catalog 命名解锁 | 已解除 | 裸名 `CatalogEntry` 已可专用于企业目录 |

下一步是在其他工作树改造收口并纳入版本控制后，补跑 `make test-platform` 与 `make test-changed`；Engine Catalog 命名不再阻塞企业 Catalog 阶段 1 设计。

## 十、2026-08-26 实施与验证记录

已通过：

- `go test ./...`：Common、System、Meta、Manager、Transfer、Service、Quality、Asset、Portal；
- `make test-common-python`：162 passed、1 skipped、8 subtests passed；
- `make test-copilot`：142 passed；
- `bash scripts/swagger/check-route-coverage.sh system develop asset copilot`：System 133、Develop 52、Asset 41、Copilot 11 个公开路由方法与生成物一致；
- `git diff --check`：通过；
- 旧公共 Go 词族、旧公开错误码、`NotebookCatalog*`、`metacatalog`、`catalogutil` 和双轨兼容路径的生产代码残留检查：无命中。

已执行但受本专题外工作阻塞：

- Develop `go test ./...`：失败于尚未收口的 `MaterializationWriteContext`、`MaterializationWriteContextRequest` 和 `ResolveMaterializationWriteContext`；编译输出中无 Engine Catalog 旧名或本迁移遗漏；
- `make test-platform`：在依赖和静态检查通过后，失败于 Makefile 已登记但尚未纳入 Git 的 `consumer-engine-recovery-online_test.py`、`consumer-process-stability-online_test.py`、`model-postgres-gate.sh` 和 `online-engine-fixture_test.py`；
- `changed-gate.py --dry-run`：已确认 `common` 变更会扩散到全部登记消费模块；由于当前工作树同时修改 CI 控制文件，计划正常选中全部模块。
